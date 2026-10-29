#!/usr/bin/env python3
"""One-image-at-a-time Gemini content production. Never called by the web app."""
import argparse
import base64
import hashlib
import io
import json
import os
from pathlib import Path
import re
import shutil
import sys
from datetime import datetime, timezone
from urllib.error import HTTPError, URLError
from urllib.request import HTTPRedirectHandler, Request, build_opener

ROOT = Path(__file__).resolve().parents[1]
MANIFEST = Path('static/assets/showcase/manifest.json')
LIMIT = 300 * 1024


class ProductionError(ValueError):
    """Safe, locally constructed diagnostic; never includes provider response bodies."""




def digest(data):
    return hashlib.sha256(data).hexdigest()


def stamp():
    return datetime.now(timezone.utc).isoformat().replace('+00:00', 'Z')


def load(root):
    return json.loads((root / MANIFEST).read_text())


def save(root, data):
    target = root / MANIFEST
    temporary = target.with_suffix('.json.tmp')
    temporary.write_text(json.dumps(data, indent=2, ensure_ascii=False) + '\n')
    temporary.replace(target)


def entry_for(data, identifier):
    if not re.fullmatch(r'day[1-4]-[1-3]', identifier):
        raise ProductionError('Invalid example ID')
    return next(e for e in data['entries'] if e['id'] == identifier)


def image_path(root, entry):
    if entry['image'] != f"/assets/showcase/{entry['id']}.jpg":
        raise ProductionError('Unexpected production image path')
    return root / 'static' / entry['image'].lstrip('/')


def request_body(root, data, entry):
    # Prompt text is sent verbatim; references are supplied separately.
    parts = [{'text': entry['prompt']}]
    hashes = {}
    for identifier in entry['references']:
        reference = entry_for(data, identifier)
        if reference['status'] != 'approved':
            raise ProductionError(f'Inspect and approve {identifier} first')
        raw = image_path(root, reference).read_bytes()
        if digest(raw) != reference.get('sha256'):
            raise ProductionError(f'Reference checksum mismatch: {identifier}')
        hashes[identifier] = digest(raw)
        parts.append({'inlineData': {'mimeType': 'image/jpeg', 'data': base64.b64encode(raw).decode()}})
    return {'contents': [{'role': 'user', 'parts': parts}],
            'generationConfig': {'responseModalities': ['TEXT', 'IMAGE'], 'imageConfig': {'aspectRatio': '4:3'}}}, hashes


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None  # Never forward credentials to another endpoint.


def call_gemini(model, key, payload):
    if not re.fullmatch(r'[a-zA-Z0-9._-]+', model):
        raise ProductionError('Use a plain Gemini model identifier')
    request = Request(f'https://generativelanguage.googleapis.com/v1beta/models/{model}:generateContent',
                      data=json.dumps(payload).encode(), method='POST',
                      headers={'Content-Type': 'application/json', 'x-goog-api-key': key})
    try:
        with build_opener(NoRedirect()).open(request, timeout=180) as response:
            raw = response.read(35 * 1024 * 1024 + 1)
    except HTTPError as error:
        raise ProductionError(f'Gemini returned HTTP {error.code}; inspect provider logs. No automatic retry.') from None
    except (URLError, TimeoutError):
        raise ProductionError('Gemini transport failed; no automatic retry.') from None
    if len(raw) > 35 * 1024 * 1024:
        raise ProductionError('Provider response exceeded the size limit')
    result = json.loads(raw)
    for candidate in result.get('candidates', []):
        for part in candidate.get('content', {}).get('parts', []):
            image = part.get('inlineData', {})
            if image.get('mimeType') in ('image/png', 'image/jpeg', 'image/webp'):
                return base64.b64decode(image['data'], validate=True), result.get('modelVersion', model)
    raise ProductionError('Gemini returned no image; inspect safety or model availability before retrying')


def web_jpeg(raw):
    from PIL import Image, ImageOps
    Image.MAX_IMAGE_PIXELS = 25_000_000
    with Image.open(io.BytesIO(raw)) as image:
        if image.width * image.height > 25_000_000:
            raise ProductionError('Generated image exceeds the pixel limit')
        original = image.size
        # Contain/letterbox preserves all visual content without stretching or cropping.
        image = ImageOps.exif_transpose(image).convert('RGB')
        fitted = ImageOps.contain(image, (800, 600), Image.Resampling.LANCZOS)
        canvas = Image.new('RGB', (800, 600), 'white')
        canvas.paste(fitted, ((800-fitted.width)//2, (600-fitted.height)//2))
        for quality in (88, 82, 76, 68):
            output = io.BytesIO()
            canvas.save(output, 'JPEG', quality=quality, optimize=True)
            if len(output.getvalue()) <= LIMIT:
                return output.getvalue(), original
    raise ProductionError('Image cannot meet the 300 KB budget; inspect it manually')


def generate(root, identifier, model):
    key = (os.getenv('GEMINI_API_KEY') or os.getenv('GOOGLE_API_KEY') or '').strip()
    if '\n' in key or '\r' in key:
        raise ProductionError('API key contains invalid whitespace')
    if not key:
        raise ProductionError('Configure GEMINI_API_KEY securely in this production environment; do not put it in git or frontend code')
    data = load(root)
    if data['published']:
        raise ProductionError('Unpublish the gallery on a branch before changing production assets')
    entry = entry_for(data, identifier)
    if entry['status'] == 'approved':
        raise ProductionError('This image is approved; invalidate dependent references before replacing it')
    payload, references = request_body(root, data, entry)
    attempt = {'number': len(entry['attempts']) + 1, 'at': stamp(), 'model': model,
               'prompt_sha256': digest(entry['prompt'].encode()), 'reference_sha256': references}
    entry['attempts'].append(attempt)
    save(root, data)
    directory = root / '.showcase-production' / identifier
    directory.mkdir(parents=True, exist_ok=True)
    try:
        raw, actual_model = call_gemini(model, key, payload)
        result, size = web_jpeg(raw)
        candidate = directory / f"attempt-{attempt['number']:03}.jpg"
        candidate.write_bytes(result)
        attempt.update(status='awaiting_visual_review', model=actual_model, sha256=digest(result), original_size=list(size))
    except Exception:
        attempt['status'] = 'failed'
        save(root, data)
        raise
    save(root, data)
    print(f'Candidate ready for visual review: {candidate}')
    print('Inspect the image, then approve explicitly or generate one new attempt. Gallery remains unpublished.')


def approve(root, identifier, number, note):
    data = load(root)
    if data['published']:
        raise ProductionError('Cannot change a published manifest')
    entry = entry_for(data, identifier)
    if entry['status'] == 'approved':
        raise ProductionError('Already approved')
    attempt = next((a for a in entry['attempts'] if a['number'] == number), None)
    if not attempt or attempt['status'] != 'awaiting_visual_review':
        raise ProductionError('Select an existing review candidate')
    if digest(entry['prompt'].encode()) != attempt['prompt_sha256']:
        raise ProductionError('Prompt changed after generation')
    for ref, expected in attempt['reference_sha256'].items():
        if digest(image_path(root, entry_for(data, ref)).read_bytes()) != expected:
            raise ProductionError('Reference changed after generation')
    candidate = root / '.showcase-production' / identifier / f'attempt-{number:03}.jpg'
    if digest(candidate.read_bytes()) != attempt['sha256']:
        raise ProductionError('Candidate checksum mismatch')
    if not note.strip():
        raise ProductionError('Record your visual-review findings')
    target = image_path(root, entry)
    shutil.copyfile(candidate, target)
    entry.update(status='approved', model=attempt['model'], sha256=attempt['sha256'],
                 selected_attempt=number, visual_review={'at': stamp(), 'note': note.strip()})
    attempt['status'] = 'selected'
    save(root, data)
    print(f'Approved {identifier}; gallery remains unpublished pending all twelve images and terms review.')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='command', required=True)
    commands.add_parser('plan')
    generate_parser = commands.add_parser('generate')
    generate_parser.add_argument('--id', required=True)
    generate_parser.add_argument('--model', required=True)
    review = commands.add_parser('approve')
    review.add_argument('--id', required=True)
    review.add_argument('--attempt', type=int, required=True)
    review.add_argument('--note', required=True)
    args = parser.parse_args()
    try:
        if args.command == 'plan':
            for entry in load(ROOT)['entries']:
                print(f"{entry['id']} [{entry['status']}] references: {', '.join(entry['references']) or 'none'}")
        elif args.command == 'generate':
            generate(ROOT, args.id, args.model)
        else:
            approve(ROOT, args.id, args.attempt, args.note)
    except ProductionError as error:
        print(str(error), file=sys.stderr)
        return 1
    except (ValueError, OSError, StopIteration):
        # Never print arbitrary upstream exception text, which may contain credentials.
        print('Production step failed. Check prerequisites, credentials, provider status and local files; no automatic retry.', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    raise SystemExit(main())

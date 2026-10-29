# Showcase production review — stage 1

These are newly proposed prompts because the previously described twelve showcase entries were absent from main. They are not recovered verbatim source content. No images have been generated. `published` remains false until visual review, terms review and asset checks are complete.

## Reviewable production sequence

### day1-1 — Photographic storytelling

A small neighbourhood bakery on a quiet Lagos street, with a green door, a wooden bread display and a person buying a loaf, at sunrise. Warm editorial photography, natural colours, eye-level composition. Landscape 4:3. No logos or text.

**Teaching point:** The subject and setting stay fixed; the photographic style makes the scene feel documentary.

**Reference inputs:** None

**Output:** `static/assets/showcase/day1-1.jpg`

### day1-2 — Watercolour storytelling

A small neighbourhood bakery on a quiet Lagos street, with a green door, a wooden bread display and a person buying a loaf, at sunrise. Hand-painted watercolour illustration on textured paper, gentle brush strokes, eye-level composition. Landscape 4:3. No logos or text.

**Teaching point:** Compare this painting with the photograph. Only the requested visual style changes.

**Reference inputs:** None

**Output:** `static/assets/showcase/day1-2.jpg`

### day1-3 — Paper-cut storytelling

A small neighbourhood bakery on a quiet Lagos street, with a green door, a wooden bread display and a person buying a loaf, at sunrise. Layered paper-cut illustration with visible paper edges and a restrained green and ochre palette, eye-level composition. Landscape 4:3. No logos or text.

**Teaching point:** The same scene becomes a graphic illustration through material and style instructions.

**Reference inputs:** None

**Output:** `static/assets/showcase/day1-3.jpg`

### day2-1 — Soft daylight

A handmade terracotta coffee cup on a pale wooden table beside a folded cream linen napkin, against a plain warm-grey wall. Eye-level product photograph, identical composition and object placement. Soft diffused daylight from the left, gentle shadows and neutral colours. Landscape 4:3. No text or logos.

**Teaching point:** Soft light reveals texture without a dramatic mood.

**Reference inputs:** None

**Output:** `static/assets/showcase/day2-1.jpg`

### day2-2 — Dramatic side light

A handmade terracotta coffee cup on a pale wooden table beside a folded cream linen napkin, against a plain warm-grey wall. Eye-level product photograph, identical composition and object placement. One hard light from the left, deep shadows to the right, dramatic high contrast. Landscape 4:3. No text or logos.

**Teaching point:** Direction remains the same; harder light creates stronger shadows.

**Reference inputs:** day2-1

**Output:** `static/assets/showcase/day2-2.jpg`

### day2-3 — Warm evening light

A handmade terracotta coffee cup on a pale wooden table beside a folded cream linen napkin, against a plain warm-grey wall. Eye-level product photograph, identical composition and object placement. Warm golden evening light from the left, soft amber highlights and long gentle shadows. Landscape 4:3. No text or logos.

**Teaching point:** Warmer colour and lower light angle change the feeling of the same product.

**Reference inputs:** day2-1

**Output:** `static/assets/showcase/day2-3.jpg`

### day3-1 — Establish the subject

The same small red delivery bicycle with a cream wicker basket, a round brass bell on the left handlebar, black tyres and a brown leather saddle. Preserve these visual anchors and the bicycle geometry. Parked outside a neighbourhood bakery with a green door. Natural editorial photograph, eye-level view, soft daylight. Landscape 4:3. No text or logos.

**Teaching point:** Establish the red frame, basket, bell and saddle before changing the setting.

**Reference inputs:** None

**Output:** `static/assets/showcase/day3-1.jpg`

### day3-2 — Change the scene

The same small red delivery bicycle with a cream wicker basket, a round brass bell on the left handlebar, black tyres and a brown leather saddle. Preserve these visual anchors and the bicycle geometry. Parked beside a leafy city park, using the supplied bicycle reference image. Natural editorial photograph, eye-level view, soft daylight. Landscape 4:3. No text or logos.

**Teaching point:** Use the first image as a reference and inspect whether the anchors survive.

**Reference inputs:** day3-1

**Output:** `static/assets/showcase/day3-2.jpg`

### day3-3 — Check consistency again

The same small red delivery bicycle with a cream wicker basket, a round brass bell on the left handlebar, black tyres and a brown leather saddle. Preserve these visual anchors and the bicycle geometry. Parked near a quiet seaside promenade, using the supplied bicycle reference image. Natural editorial photograph, eye-level view, soft daylight. Landscape 4:3. No text or logos.

**Teaching point:** A third scene tests continuity; small differences should be discussed rather than hidden.

**Reference inputs:** day3-1

**Output:** `static/assets/showcase/day3-3.jpg`

### day4-1 — Start a product composition

A plain reusable sage-green water bottle with a rounded silver cap, on a pale stone pedestal. Soft studio photograph with several green leaves and textured fabric in the background. The bottle is centred. Landscape 4:3. No text, lettering or logos.

**Teaching point:** This initial composition is the starting point for two deliberate edits.

**Reference inputs:** None

**Output:** `static/assets/showcase/day4-1.jpg`

### day4-2 — Edit the background

Edit the supplied product photograph. Preserve the sage-green bottle, rounded silver cap, pedestal and their proportions. Remove the leaves and fabric, replace them with a simple warm-grey background, and move the bottle to the left third, leaving clear space on the right for a future headline. Keep the landscape 4:3 framing. Do not add text or logos.

**Teaching point:** First edit: simplify distractions and make space for a landing-page headline.

**Reference inputs:** day4-1

**Output:** `static/assets/showcase/day4-2.jpg`

### day4-3 — Refine the light

Edit the supplied simplified product photograph. Preserve the sage-green bottle, rounded silver cap, pedestal, warm-grey background and left-third placement. Soften the shadows and add a subtle warm highlight along the left edge of the bottle. Keep the empty right-hand space and landscape 4:3 framing. Do not add text or logos.

**Teaching point:** Second edit: refine lighting while preserving the composition already achieved.

**Reference inputs:** day4-2

**Output:** `static/assets/showcase/day4-3.jpg`

## Next stage

Confirm this prompt set, then use a securely configured Gemini API key in the content-production environment. Do not put the key in frontend assets, git, or the Render student runtime. Select and record a currently supported image model and its exact version after checking current API documentation. Generate sequentially, preserve prompts, supply the listed reference images, inspect every output, and record attempts in the manifest. A filtered or failed image is not automatically retried indefinitely: inspect the reason and stop for review.

Generate at 4:3 when supported. Resize proportionally to 800×600 and inspect crop/composition before approval. Target no more than 200 KB per final JPEG, with a hard 300 KB limit. Do not stretch images or remove required source markings. Record model, attempts, reference IDs and SHA-256 for each accepted image. Label outputs as AI-generated examples; do not imply guaranteed learner results or student endorsements.

Review current Gemini API terms and model-specific conditions before publication. Terms review is a recorded publishing prerequisite, not a claim that Google guarantees commercial rights or exclusive outputs.

## Release gate

All twelve entries must be approved, every expected asset must decode at 800×600 and stay within the byte limit, terms review must be recorded, and the gallery/browser tests must pass. Then change `published` to true on a branch, wait for CI, and merge. Check each lesson, each image and mobile/desktop layouts. Preserve the fallback for unexpected asset failures.

The workflow for each stage is branch → push → passing CI → merge. Commit and merge messages must not contain co-authored trailers. Obtain the user's review between content-production stages.

## Follow-up product scope

Private student uploads (My Creations) and the consent-based public student wall remain separate work. This stage creates only curated course-example infrastructure. Tier 1 copy focuses on turning a course image into a landing page and discloses that external tools may have separate charges; it makes no unverified market-price claims.

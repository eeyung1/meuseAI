import base64
import contextlib
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from PIL import Image
import showcase_production as production


def picture():
    output=io.BytesIO()
    Image.new('RGB',(1200,900),'blue').save(output,'PNG')
    return output.getvalue()


class ProductionTests(unittest.TestCase):
    def setUp(self):
        self.directory=tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root=Path(self.directory.name)
        target=self.root/production.MANIFEST
        target.parent.mkdir(parents=True)
        self.data={'published':False,'entries':[
            {'id':'day1-1','prompt':'Exact original prompt.','image':'/assets/showcase/day1-1.jpg','status':'pending','references':[],'attempts':[]},
            {'id':'day1-2','prompt':'Edit the reference.','image':'/assets/showcase/day1-2.jpg','status':'pending','references':['day1-1'],'attempts':[]}]}
        production.save(self.root,self.data)

    def test_missing_credentials_never_calls_provider(self):
        with patch.dict('os.environ',{},clear=True),patch.object(production,'call_gemini') as call:
            with self.assertRaisesRegex(production.ProductionError,'GEMINI_API_KEY'):
                production.generate(self.root,'day1-1','test-model')
            call.assert_not_called()
        self.assertEqual(production.load(self.root),self.data)

    def test_generate_review_and_reference_pipeline(self):
        def provider(model,key,payload):
            self.assertEqual(payload['contents'][0]['parts'][0]['text'],'Exact original prompt.')
            self.assertEqual(payload['generationConfig']['imageConfig']['aspectRatio'],'4:3')
            return picture(),'model-version-1'
        with patch.dict('os.environ',{'GEMINI_API_KEY':'private-key'}),patch.object(production,'call_gemini',side_effect=provider),contextlib.redirect_stdout(io.StringIO()):
            production.generate(self.root,'day1-1','test-model')
        data=production.load(self.root);entry=data['entries'][0]
        self.assertEqual(entry['status'],'pending')
        self.assertEqual(entry['attempts'][0]['status'],'awaiting_visual_review')
        self.assertFalse(production.image_path(self.root,entry).exists())
        self.assertNotIn('private-key',json.dumps(data))
        with contextlib.redirect_stdout(io.StringIO()):
            production.approve(self.root,'day1-1',1,'Inspected subject, composition and image quality.')
        data=production.load(self.root)
        self.assertFalse(data['published'])
        self.assertEqual(data['entries'][0]['status'],'approved')
        with Image.open(production.image_path(self.root,data['entries'][0])) as image:
            self.assertEqual(image.size,(800,600))
        payload,hashes=production.request_body(self.root,data,data['entries'][1])
        self.assertEqual(payload['contents'][0]['parts'][0]['text'],'Edit the reference.')
        self.assertEqual(payload['contents'][0]['parts'][1]['inlineData']['mimeType'],'image/jpeg')
        self.assertIn('day1-1',hashes)

    def test_reference_must_be_approved(self):
        with self.assertRaisesRegex(production.ProductionError,'approve day1-1'):
            production.request_body(self.root,self.data,self.data['entries'][1])

    def test_failed_generation_records_attempt_without_credentials(self):
        with patch.dict('os.environ',{'GEMINI_API_KEY':'private-key'}),patch.object(production,'call_gemini',side_effect=production.ProductionError('Gemini returned no image')):
            with self.assertRaises(production.ProductionError):
                production.generate(self.root,'day1-1','test-model')
        data=production.load(self.root)
        self.assertEqual(data['entries'][0]['attempts'][0]['status'],'failed')
        self.assertNotIn('private-key',json.dumps(data))

    def test_changed_prompt_cannot_be_approved(self):
        with patch.dict('os.environ',{'GEMINI_API_KEY':'private-key'}),patch.object(production,'call_gemini',return_value=(picture(),'version-1')),contextlib.redirect_stdout(io.StringIO()):
            production.generate(self.root,'day1-1','test-model')
        data=production.load(self.root);data['entries'][0]['prompt']='Changed prompt';production.save(self.root,data)
        with self.assertRaisesRegex(production.ProductionError,'Prompt changed'):
            production.approve(self.root,'day1-1',1,'Looks good')

    def test_path_and_model_cannot_redirect_key(self):
        with self.assertRaises(production.ProductionError):
            production.call_gemini('https://attacker.example','private-key',{})
        with self.assertRaises(production.ProductionError):
            production.entry_for(self.data,'../../secret')


if __name__=='__main__':
    unittest.main()

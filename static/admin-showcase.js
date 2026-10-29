(function (host) {
 'use strict';
 function create({document,fetch,getToken,onUnauthorized,clipboard,confirm,URL}) {
  const $=id=>document.getElementById(id);let previewURLs=[],busy=false,revision=0;
  function note(text){$('showcase-admin-note').textContent=text;}
  function clear(){revision++;for(const url of previewURLs)URL.revokeObjectURL(url);previewURLs=[];$('showcase-admin-grid').replaceChildren();}
  async function request(path,options={}){
   const token=getToken();if(!token)throw Error('Sign in as admin first.');
   const response=await fetch(path,{...options,headers:{...options.headers,Authorization:'Bearer '+token}});
   if(response.status===401){clear();onUnauthorized();throw Error('Admin session expired.');}
   if(!response.ok){let message='Gallery request failed; please retry.';try{message=(await response.json()).error||message;}catch(_){}throw Error(message);}
   return response;
  }
  function element(tag,text,cls){const e=document.createElement(tag);if(text)e.textContent=text;if(cls)e.className=cls;return e;}
  async function load(){
   if(!getToken())return;clear();const current=revision;
   try{
    const data=await (await request('/api/v1/admin/showcase')).json();if(current!==revision)return;
    const entries=data.entries||[],approved=entries.filter(e=>e.status==='approved').length;
    $('showcase-admin-state').textContent=`${approved} / 12 approved · ${data.published?'Published':'Private drafts'}`;
    $('showcase-publish-btn').disabled=data.published||approved!==12;$('showcase-unpublish-btn').disabled=!data.published;
    const grid=$('showcase-admin-grid');
    for(const entry of entries){
     const card=element('article',null,'showcase-admin-card');card.append(element('h3',`Day ${entry.day} · ${entry.title}`),element('p',`${entry.id} · ${entry.status}`,'muted'));
     const prompt=element('textarea');prompt.value=entry.prompt;prompt.readOnly=true;prompt.rows=5;prompt.setAttribute('aria-label',`Prompt for ${entry.id}`);card.append(prompt);
     const copy=element('button','Copy prompt','btn-secondary-sm');copy.type='button';copy.addEventListener('click',async()=>{try{await clipboard.writeText(entry.prompt);note(`${entry.id}: prompt copied. Generate it manually and upload the result below.`);}catch(_){prompt.focus();prompt.select();note('Select and copy the prompt manually; automatic copying was blocked.');}});card.append(copy);
     card.append(element('p',entry.explanation,'muted'));
     const refs=element('p',entry.references.length?`Reference images required: ${entry.references.join(', ')}. Download and attach them in your image tool.`:'No reference image required.','muted');card.append(refs);
     for(const reference of entry.references){const download=element('button',`Download ${reference}`,'btn-secondary-sm');download.type='button';download.addEventListener('click',async()=>{try{const blob=await (await request(`/api/v1/admin/showcase/${reference}/image`)).blob();const url=URL.createObjectURL(blob);previewURLs.push(url);const link=element('a');link.href=url;link.download=reference+'.jpg';link.click();}catch(error){note(error.message);}});card.append(download);}
     if(entry.has_image){
      const image=element('img');image.alt=`Uploaded preview for ${entry.title}`;image.width=800;image.height=600;card.append(image);
      // Private previews are fetched with the admin header, never a token in a URL.
      request(`/api/v1/admin/showcase/${entry.id}/image`).then(r=>r.blob()).then(blob=>{if(current!==revision)return;const url=URL.createObjectURL(blob);previewURLs.push(url);image.src=url;}).catch(error=>{if(current===revision)note(error.message);});
     }
     const source=element('select');source.setAttribute('aria-label',`Generation source for ${entry.id}`);
     for(const name of ['Gemini','ChatGPT','Other']){const option=element('option',name);option.value=name;source.append(option);}source.value=entry.source||'Gemini';source.disabled=data.published;
     const file=element('input');file.type='file';file.accept='image/jpeg,image/png';file.disabled=data.published;file.setAttribute('aria-label',`Upload image for ${entry.id}`);
     const upload=element('button',entry.has_image?'Replace image':'Upload image','btn-primary-sm');upload.type='button';upload.disabled=data.published;
     upload.addEventListener('click',async()=>{
      if(busy)return;const selected=file.files?.[0];if(!selected){note('Select a JPEG or PNG first.');return;}if(selected.size>8*1024*1024){note('The image must be no larger than 8 MB.');return;}
      if(entry.has_image&&!confirm('Replace this image? Approval of this image and its dependent examples will be reset.'))return;
      busy=true;upload.disabled=true;
      try{const form=new FormData();form.append('image',selected);form.append('source',source.value);await request(`/api/v1/admin/showcase/${entry.id}/upload`,{method:'POST',body:form});note(`${entry.id} uploaded. Inspect the normalized preview, then approve it.`);await load();}catch(error){note(error.message);}finally{busy=false;upload.disabled=false;}
     });card.append(element('label','Generation source'),source,element('label','JPEG/PNG, at least 800×600, up to 8 MB and 16 megapixels. Automatically fitted to 800×600 without cropping.'),file,upload);
     if(entry.has_image&&entry.status!=='approved'&&!data.published){
      const review=element('textarea');review.rows=2;review.placeholder='Visual review: prompt fidelity, teaching point, quality and composition';review.setAttribute('aria-label',`Visual review for ${entry.id}`);
      const rights=element('input');rights.type='checkbox';const rightsLabel=element('label','I generated this image or have permission to display it, and checked the source tool’s usage terms.');rightsLabel.prepend(rights);
      const approve=element('button','Approve this image','btn-secondary-sm');approve.type='button';approve.addEventListener('click',async()=>{if(busy)return;if(!rights.checked||review.value.trim().length<5){note('Add your review note and confirm image rights.');return;}busy=true;approve.disabled=true;try{await request(`/api/v1/admin/showcase/${entry.id}/review`,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({sha256:entry.sha256,note:review.value,rights_confirmed:rights.checked})});note(`${entry.id} approved.`);await load();}catch(error){note(error.message);}finally{busy=false;approve.disabled=false;}});card.append(review,rightsLabel,approve);
     }
     grid.append(card);
    }
   }catch(error){if(current===revision)note(error.message);}
  }
  async function publish(published){
   if(busy)return;
   const rights=$('showcase-rights-confirmed').checked,noteValue=$('showcase-rights-note').value.trim();
   if(published&&(!rights||noteValue.length<10)){note('Confirm usage rights and add a terms-review note before publishing.');return;}
   busy=true;try{await request('/api/v1/admin/showcase/publish',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({published,rights_confirmed:rights,note:noteValue})});note(published?'Gallery published. Students will see it on their next refresh.':'Gallery unpublished. Drafts remain saved and can be edited.');await load();}catch(error){note(error.message);}finally{busy=false;}
  }
  $('showcase-refresh-btn')?.addEventListener('click',load);
  $('showcase-publish-btn')?.addEventListener('click',()=>publish(true));
  $('showcase-unpublish-btn')?.addEventListener('click',()=>publish(false));
  return {load,clear};
 }
 if(typeof module!=='undefined'&&module.exports)module.exports={create};else host.MeuseAdminShowcase={create};
})(typeof window!=='undefined'?window:globalThis);

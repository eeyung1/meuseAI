/* Apply a validated local appearance before styles paint; no account data is stored. */
(() => {
  'use strict';
  const key='meuseai-appearance';
  const valid=value=>['dark','light','system'].includes(value);
  const media=typeof window.matchMedia==='function'?window.matchMedia('(prefers-color-scheme: dark)'):null;
  let preference='dark';
  try{const saved=localStorage.getItem(key);if(valid(saved))preference=saved;}catch(_){}
  function apply(){
    const actual=preference==='system'?(media?.matches?'dark':'light'):preference;
    document.documentElement.setAttribute('data-appearance',actual);
    for(const control of document.querySelectorAll('[data-appearance-control]'))control.value=preference;
  }
  apply();
  function initialize(){
    for(const control of document.querySelectorAll('[data-appearance-control]')){
      control.value=preference;
      control.addEventListener('change',()=>{
        if(!valid(control.value))return;
        preference=control.value;
        try{localStorage.setItem(key,preference);}catch(_){}
        apply();
      });
    }
  }
  if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',initialize,{once:true});else initialize();
  const changed=()=>{if(preference==='system')apply();};
  if(media?.addEventListener)media.addEventListener('change',changed);else media?.addListener?.(changed);
  window.addEventListener('storage',event=>{if(event.key===key){preference=valid(event.newValue)?event.newValue:'dark';apply();}});
})();

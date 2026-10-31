(() => {
 'use strict';
 const $=id=>document.getElementById(id);
 let attempts=0,timer=null,loading=false;
 async function check(){
  if(loading)return;
  loading=true;clearTimeout(timer);$('check-payment').disabled=true;
  try{
   const response=await fetch('/api/v1/dashboard/me',{credentials:'same-origin',headers:{Accept:'application/json'},cache:'no-store'});
   if(response.status===401){$('payment-state').textContent='Sign in with the email used for your purchase to check payment confirmation.';$('tier1-sign-in').hidden=false;return;}
   if(!response.ok)throw Error('Unable to check payment right now. Please try again or contact us.');
   const data=await response.json(),tracks=[];
   if(data.tier1_landing)tracks.push('Lovable landing pages');
   if(data.tier1_video)tracks.push('AI video with CapCut');
   $('tier1-confirmed').hidden=!tracks.length;
   if(tracks.length){$('payment-state').textContent='Payment confirmed. Welcome to Tier 1.';$('purchased-tracks').textContent='Your purchased tracks: '+tracks.join(' and ')+'.';}
   else{$('payment-state').textContent='Payment confirmation is pending. If you completed checkout, please wait a moment. Do not pay again; check again or contact us.';if(++attempts<12)timer=setTimeout(check,3000);}
  }catch(error){$('payment-state').textContent=error.message;}
  finally{loading=false;$('check-payment').disabled=false;}
 }
 $('check-payment').addEventListener('click',()=>{attempts=0;check();});
 window.addEventListener('pagehide',()=>clearTimeout(timer));
 check();
})();

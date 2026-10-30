const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
function harness(saved,{dark=false,blocked=false}={}){
 const root={},events={},windows={},control={value:'',addEventListener:(name,fn)=>events[name]=fn},storage=new Map(saved===undefined?[]:[['meuseai-appearance',saved]]);let ready=false,mediaChanged;
 const media={matches:dark,addEventListener:(name,fn)=>mediaChanged=fn};
 const document={readyState:'loading',documentElement:{setAttribute:(key,value)=>root[key]=value},querySelectorAll:()=>ready?[control]:[],addEventListener:(name,fn)=>events[name]=fn};
 const context={document,window:{matchMedia:()=>media,addEventListener:(name,fn)=>windows[name]=fn},localStorage:{getItem:key=>{if(blocked)throw Error('blocked');return storage.get(key);},setItem:(key,value)=>{if(blocked)throw Error('blocked');storage.set(key,value);}}};
 vm.runInNewContext(fs.readFileSync(__dirname+'/appearance.js','utf8'),context);
 const initial=root['data-appearance'];ready=true;events.DOMContentLoaded();
 return{root,initial,control,storage,choose:value=>{control.value=value;events.change();},system:value=>{media.matches=value;mediaChanged();},storageChange:value=>windows.storage({key:'meuseai-appearance',newValue:value})};
}
test('default uses light orange and saved dark applies before controls initialize',()=>{assert.equal(harness().initial,'light');const h=harness('dark');assert.equal(h.initial,'dark');assert.equal(h.control.value,'dark');});
test('switching appearance persists the choice',()=>{const h=harness();h.choose('light');assert.equal(h.root['data-appearance'],'light');assert.equal(h.storage.get('meuseai-appearance'),'light');assert.equal(harness(h.storage.get('meuseai-appearance')).initial,'light');});
test('system appearance follows device changes only when selected',()=>{const h=harness('system',{dark:true});assert.equal(h.initial,'dark');h.system(false);assert.equal(h.root['data-appearance'],'light');h.choose('dark');h.system(false);assert.equal(h.root['data-appearance'],'dark');});
test('storage restrictions do not prevent changing the current page',()=>{const h=harness('light',{blocked:true});assert.equal(h.initial,'light');h.choose('light');assert.equal(h.root['data-appearance'],'light');});
test('only validated values can affect the root and tabs synchronize',()=>{const h=harness('javascript:bad');assert.equal(h.initial,'light');h.choose('unknown');assert.equal(h.root['data-appearance'],'light');h.storageChange('light');assert.equal(h.root['data-appearance'],'light');assert.equal(h.control.value,'light');h.storageChange(null);assert.equal(h.root['data-appearance'],'light');});
test('all pages include the early script, selector and shared style override',()=>{for(const page of ['landing','dashboard','verify','share','admin']){const html=fs.readFileSync(__dirname+'/'+page+'.html','utf8');assert.ok(html.indexOf('/assets/appearance.js')<html.indexOf('rel="stylesheet"'));assert.ok(html.indexOf('/assets/appearance.css')>html.indexOf('rel="stylesheet"'));assert.match(html,/data-appearance-control aria-label="Appearance"/);}});
test('core light palette text and button colours meet 4.5:1 contrast',()=>{
 const css=fs.readFileSync(__dirname+'/appearance.css','utf8');const palette=css.match(/:root\[data-appearance=light\]\{([^}]+)\}/)[1];const vars=Object.fromEntries([...palette.matchAll(/--([\w-]+):\s*(#[a-f\d]{6})/gi)].map(m=>[m[1],m[2]]));
 const lum=c=>c.slice(1).match(/../g).map(x=>parseInt(x,16)/255).map(x=>x<=.04045?x/12.92:((x+.055)/1.055)**2.4).reduce((n,v,i)=>n+v*[.2126,.7152,.0722][i],0);
 for(const [a,b] of [[vars.text,vars.bg],[vars.muted,vars.surface],[vars.yellow,'#ffffff'],[vars.green,vars.surface]]){const levels=[lum(a),lum(b)].sort((a,b)=>a-b);assert.ok((levels[1]+.05)/(levels[0]+.05)>=4.5,a+' vs '+b);}
});

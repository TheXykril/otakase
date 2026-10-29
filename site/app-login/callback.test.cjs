const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync(__dirname + '/callback.js', 'utf8');
const state = extra => Buffer.from(JSON.stringify({nonce:'a'.repeat(64),service:'AniList',...extra})).toString('base64url');
function run(query, hash = '') {
  const elements = {message:{}, continue:{hidden:true}};
  const navigations = [], order = [];
  const location = {search:query,hash,pathname:'/app-login/',assign:url=>{order.push('navigate');navigations.push(url);}};
  vm.runInNewContext(source,{location,history:{replaceState:()=>order.push('erase')},document:{getElementById:key=>elements[key]},URLSearchParams,atob:s=>Buffer.from(s,'base64').toString('binary')});
  return {elements,navigations,order};
}
test('phone implicit fragment is cleared and transferred through registered scheme',()=>{
  const s=state({});const r=run('',`#access_token=token&state=${s}&expires_in=31536000`);
  assert.deepEqual(r.order,['erase','navigate']);const url=new URL(r.navigations[0]);assert.equal(url.protocol,'otakase:');assert.equal(url.hostname,'login');assert.equal(url.searchParams.get('access_token'),'token');assert.equal(r.elements.continue.hidden,false);
});
test('TV waits for a button, then uses top-level LAN navigation',()=>{
  const s=state({host:'192.168.1.7',port:50001});const r=run('',`#access_token=token&state=${s}`);
  assert.equal(r.navigations.length,0);r.elements.continue.onclick();assert.equal(new URL(r.navigations[0]).origin,'http://192.168.1.7:50001');assert.equal(new URL(r.navigations[0]).pathname,'/login');
});
test('MAL authorization code and state survive phone redirect',()=>{
  const s=state({service:'MAL'});const r=run(`?code=approved&state=${s}`);assert.equal(new URL(r.navigations[0]).searchParams.get('code'),'approved');
});
test('invalid state, public address, metadata host and missing response never navigate',()=>{
  for(const s of ['invalid',state({host:'169.254.169.254',port:50001}),state({host:'8.8.8.8',port:50001}),state({host:'192.168.1.1.evil',port:50001}),state({host:'192.168.1.7',port:80})]){
    const r=run(`?access_token=token&state=${s}`);assert.equal(r.elements.continue.hidden,true);assert.equal(r.navigations.length,0);assert.deepEqual(r.order,['erase']);
  }
  assert.equal(run(`?state=${state({})}`).navigations.length,0);
});
test('declined login never forwards a token or code',()=>{
  const r=run(`?error=access_denied&state=${state({})}`);assert.equal(r.navigations.length,0);assert.equal(r.elements.continue.hidden,true);
});

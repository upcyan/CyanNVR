import assert from 'node:assert/strict'
import test from 'node:test'
import fs from 'node:fs'
import vm from 'node:vm'
import ts from 'typescript'

function load(path, deps, globals={}) {
 const code=ts.transpileModule(fs.readFileSync(new URL(path,import.meta.url),'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText
 const module={exports:{}}
 vm.runInNewContext(code,{module,exports:module.exports,require:n=>deps[n],URL,Promise,console,...globals})
 return module.exports
}
function setup(){
 const values=new Map([['nvr_token','old']]);const storage={getItem:k=>values.get(k)||null,setItem:(k,v)=>values.set(k,v)}
 let request,success,failure;let epoch=1;const renewed=[];const lost=[]
 const http={interceptors:{request:{use:f=>request=f},response:{use:(a,b)=>{success=a;failure=b}}}}
 const server={base:'http://nvr.test'}
 const client=load('./src/api/client.ts',{axios:{default:{create:()=>http}},'./server':{server}},{localStorage:storage,location:{hash:'#/live'}})
 client.bindSessionHooks({epoch:()=>epoch,onAuthLost:e=>lost.push(e),onTokenRenewed:t=>renewed.push(t)})
 return {storage,server,http,request,success,failure,renewed,lost,setEpoch:v=>epoch=v}
}
test('current renewal accepted, late old renewal ignored',()=>{
 const s=setup();const config=s.request({headers:{}})
 s.success({config,headers:{'x-renewed-token':'new'}});assert.deepEqual(s.renewed,['new'])
 s.storage.setItem('nvr_token','new-login');s.setEpoch(2)
 s.success({config,headers:{'x-renewed-token':'stale'}});assert.deepEqual(s.renewed,['new'])
})
test('late 401 cannot invalidate renewed or switched session',async()=>{
 const s=setup();const config=s.request({headers:{}})
 s.storage.setItem('nvr_token','renewed')
 await assert.rejects(s.failure({config,response:{status:401}}));assert.equal(s.lost.length,0)
 s.storage.setItem('nvr_token','old');s.server.base='http://other.test'
 await assert.rejects(s.failure({config,response:{status:401}}));assert.equal(s.lost.length,0)
})
test('current-session 401 invalidates exactly current epoch',async()=>{
 const s=setup();const config=s.request({headers:{}})
 await assert.rejects(s.failure({config,response:{status:401}}));assert.deepEqual(s.lost,[1])
})
test('manifest/segment URL uses latest token, never sends it to unrelated origin',()=>{
 const s=setup();const stream=load('./src/utils/streamSession.ts',{'../api/client':{http:s.http},'../api/server':{server:s.server}},{localStorage:s.storage,window:{location:{href:'http://nvr.test/#/live',origin:'http://nvr.test'}}})
 s.storage.setItem('nvr_token','new')
 assert.equal(new URL(stream.currentStreamUrl('http://nvr.test/api/stream/live/x/a.ts?token=old')).searchParams.get('token'),'new')
 assert.equal(stream.currentStreamUrl('https://elsewhere.test/api/x?token=old'),'https://elsewhere.test/api/x?token=old')
})
test('stream auth probe coalesces requests and distinguishes network errors from 401',async()=>{
 const s=setup();let calls=0;let reject
 s.http.get=()=>{calls++;return new Promise((_,r)=>reject=r)}
 const stream=load('./src/utils/streamSession.ts',{'../api/client':{http:s.http},'../api/server':{server:s.server}},{localStorage:s.storage,window:{location:{href:'http://nvr.test/#/live',origin:'http://nvr.test'}}})
 const a=stream.checkStreamSession();const b=stream.checkStreamSession();assert.equal(a,b);assert.equal(calls,1)
 reject({response:{status:401}});assert.equal(await a,false)
 const c=stream.checkStreamSession();reject({});assert.equal(await c,true)
})

test('SSE current invalid session stops retries; disconnected old error cannot reconnect',async()=>{
 let valid=false;let checks=0;let timers=0
 class Source{constructor(url){this.url=url}close(){this.closed=true}}
 const s=setup()
 const notify=load('./src/utils/notify.ts',{'vue':{ref:value=>({value})},'../api/server':{server:s.server},'../stores/settings':{useSettingsStore:()=>({settings:{}})},'./streamSession':{checkStreamSession:async()=>{checks++;return valid}}},{localStorage:s.storage,EventSource:Source,setTimeout:()=>{timers++;return timers},clearTimeout:()=>{},Notification:undefined})
 // 捕获实例，以检查断开后的迟到回调。
 let source
 const Old=Source
 const globals={localStorage:s.storage,EventSource:class extends Old{constructor(url){super(url);source=this}},setTimeout:()=>{timers++;return timers},clearTimeout:()=>{},Notification:undefined}
 const n=load('./src/utils/notify.ts',{'vue':{ref:value=>({value})},'../api/server':{server:s.server},'../stores/settings':{useSettingsStore:()=>({settings:{}})},'./streamSession':{checkStreamSession:async()=>{checks++;return valid}}},globals).useNotifications()
 n.connect();await source.onerror();assert.equal(timers,0);assert.equal(source.closed,true)
 n.disconnect();valid=true;n.connect();const old=source;n.disconnect();await old.onerror();assert.equal(timers,0)
 assert.equal(checks,1)
})
test('HLS 401 stops loads, invalid session destroys resources',async()=>{
 const s=setup();let stops=0;let destroys=0
 const player=load('./src/utils/player.ts',{'./streamSession':{currentStreamUrl:x=>x,checkStreamSession:async()=>false}},{localStorage:s.storage,window:{clearTimeout:()=>{},removeEventListener:()=>{}},document:{removeEventListener:()=>{}}})
 const p=new player.HlsStream('http://nvr.test/api/x?token=old')
 p.video={pause(){},removeAttribute(){}}
 p.hls={stopLoad(){stops++},destroy(){destroys++}}
 p.onHlsError({response:{code:401}});await new Promise(resolve=>setImmediate(resolve))
 assert.equal(stops,1);assert.equal(destroys,1);assert.equal(p.video,null)
})
test('HLS stale auth probe cannot rebuild replacement connection',async()=>{
 const s=setup();let finish;let rebuilt=0
 const player=load('./src/utils/player.ts',{'./streamSession':{currentStreamUrl:x=>x,checkStreamSession:()=>new Promise(r=>finish=r)}},{localStorage:s.storage,window:{clearTimeout:()=>{},removeEventListener:()=>{}},document:{removeEventListener:()=>{}}})
 const p=new player.HlsStream('http://nvr.test/api/x')
 p.video={};p.hls={stopLoad(){},destroy(){}};p.rebuild=()=>rebuilt++
 p.onHlsError({response:{code:401}});p.generation++;finish(true)
 await new Promise(resolve=>setImmediate(resolve));assert.equal(rebuilt,0)
})

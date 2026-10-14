import assert from 'node:assert/strict'
import test from 'node:test'
import fs from 'node:fs'
import vm from 'node:vm'
import ts from 'typescript'

const code=ts.transpileModule(fs.readFileSync(new URL('./src/utils/version.ts',import.meta.url),'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText
const module={exports:{}};vm.runInNewContext(code,{module,exports:module.exports})
const {aboutVersion}=module.exports

test('app version wins inside the app',()=>{
 const r=aboutVersion('1.10.26','1.10.32')
 assert.equal(r.version,'1.10.26');assert.equal(r.source,'app')
})
test('browser falls back to server version',()=>{
 const r=aboutVersion('','1.10.32')
 assert.equal(r.version,'1.10.32');assert.equal(r.source,'server')
 const u=aboutVersion(undefined,'1.10.32');assert.equal(u.version,'1.10.32')
})
test('both missing hides the line',()=>{
 const r=aboutVersion();assert.equal(r.version,'');assert.equal(r.source,'none')
})

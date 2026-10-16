import assert from 'node:assert/strict'
import test from 'node:test'

// 复刻 PlaybackView 的分组时长算法（钳制版），锁死「无交集段丢弃 + 时长钳制」契约。
function durationMin(start,end,ds){
 const de=ds+86400000
 const a=Math.max(start,ds), b=Math.min(end,de)
 if(b<=a) return null
 return Math.min(Math.max((b-a)/60000,0),1440)
}

test('segment entirely outside the day is dropped (was -322259 min bug)',()=>{
 const ds=new Date('2026-10-16T00:00:00').getTime()
 const staleStart=new Date('2026-03-06T00:00:00').getTime()
 const staleEnd=staleStart+5*60000
 assert.equal(durationMin(staleStart,staleEnd,ds),null)
})
test('crossing-midnight segment counts only the visible part',()=>{
 const ds=new Date('2026-10-16T00:00:00').getTime()
 const s=new Date('2026-10-15T23:59:00').getTime()
 const e=new Date('2026-10-16T00:04:00').getTime()
 assert.equal(durationMin(s,e,ds),4)
})
test('absurd long segment clamped to 24h',()=>{
 const ds=new Date('2026-10-16T00:00:00').getTime()
 assert.equal(durationMin(ds,ds+3*86400000,ds),1440)
})
test('normal 5-min segment unchanged',()=>{
 const ds=new Date('2026-10-16T00:00:00').getTime()
 assert.equal(durationMin(ds,ds+5*60000,ds),5)
})
test('date-of-window picks the day with max overlap',()=>{
 // 复刻 onTimelineRange 的日期跟随逻辑
 const dayStart=new Date('2026-10-14T00:00:00').getTime()
 const p2=(n)=>String(n).padStart(2,'0')
 function follow(r){
  const candidates=[-2,-1,0,1,2,3,4,5,6,7].map(k=>dayStart+k*86400000)
  let best=dayStart,bestOv=-1
  for(const ds of candidates){
   const ov=Math.min(r.to,ds+86400000)-Math.max(r.from,ds)
   if(ov>bestOv){bestOv=ov;best=ds}
  }
  if(best!==dayStart&&bestOv>0){const d=new Date(best);return `${d.getFullYear()}-${p2(d.getMonth()+1)}-${p2(d.getDate())}`}
  return null
 }
 // 拖到 10-16（原日期 10-14，窗口一整天）
 const from=new Date('2026-10-16T00:00:00').getTime()
 assert.equal(follow({from,to:from+86400000}),'2026-10-16')
 // 跨过午夜：窗口主体（20h）落在 10-15 → 跟随到 10-15
 const f2=new Date('2026-10-14T20:00:00').getTime()
 assert.equal(follow({from:f2,to:f2+86400000}),'2026-10-15')
})
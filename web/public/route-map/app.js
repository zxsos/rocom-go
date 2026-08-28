"use strict";
// 跑图收集路线:底图 4096(map.webp),路线坐标 8192 画布,归一化映射。
(() => {
const GRID=8192, MAP=4096, K=MAP/GRID, OX=306000, OY=408000, SIDE=408000;
const cv=document.getElementById("map"), cx=cv.getContext("2d");
const listEl=document.getElementById("routeList");
let view={scale:1,ox:0,oy:0}, dpr=window.devicePixelRatio||1;
const toS=(mx,my)=>[mx*view.scale+view.ox,my*view.scale+view.oy];
const toM=(sx,sy)=>[(sx-view.ox)/view.scale,(sy-view.oy)/view.scale];
function fit(){const w=cv.clientWidth,h=cv.clientHeight;view.scale=Math.min(w,h)/MAP*.95;view.ox=(w-MAP*view.scale)/2;view.oy=(h-MAP*view.scale)/2;}
function resize(){dpr=window.devicePixelRatio||1;cv.width=Math.round(cv.clientWidth*dpr);cv.height=Math.round(cv.clientHeight*dpr);cx.setTransform(dpr,0,0,dpr,0,0);draw();}
const PAL=["#e63946","#499ed5","#26890c","#ffb000","#9c27b0","#00897b","#ff5722","#795548","#3f51b5","#00796b","#e91e63","#18ffff","#8bc34a","#fff176","#ab47bc","#607d8b","#ff7043","#1de9b6","#d500f9","#c5e1a5","#ff6f00","#00bcd4","#d32f2f","#76ff03","#7c4dff","#26a69a","#ef5350","#5c6bc0","#ffca28","#42a5f5","#ef6c00","#66bb6a","#ec407a","#29b6f6"];
let routes=[],loading=0;
async function load(){
  const list=await fetch("data/index.json",{cache:"no-store"}).then(r=>r.json());
  for(const name of list){
    loading++;
    const d=await fetch("data/"+encodeURIComponent(name),{cache:"no-store"}).then(r=>r.json());
    routes.push({name,count:d.points.length,points:d.points,color:PAL[routes.length%PAL.length],on:false});
    loading--;render();if(!loading)draw();
  }
}
function render(){
  listEl.innerHTML="";
  routes.forEach(r=>{
    const it=document.createElement("div");it.className="route-item"+(r.on?" active":"");
    const sw=document.createElement("span");sw.className="swatch";sw.style.background=r.color;
    const nm=document.createElement("span");nm.className="rname";nm.textContent=r.name.replace(/\.json$/,"");nm.title=r.name;
    const ct=document.createElement("span");ct.className="rcount";ct.textContent=r.count;
    const cb=document.createElement("input");cb.type="checkbox";cb.checked=r.on;
    cb.addEventListener("change",()=>{r.on=cb.checked;it.classList.toggle("active",r.on);draw();sel();});
    it.addEventListener("click",e=>{if(e.target===cb)return;routes.forEach(o=>o.on=false);r.on=true;render();draw();sel();});
    it.append(sw,nm,ct,cb);listEl.appendChild(it);
  });
}
function sel(){document.getElementById("sel").textContent="已选 "+routes.filter(r=>r.on).length+" 条";}
const bg=new Image();bg.src="map.webp";
function draw(){
  const w=cv.clientWidth,h=cv.clientHeight;cx.clearRect(0,0,w,h);cx.save();
  const a=parseFloat(document.getElementById("bgOpacity").value);
  if(bg.complete){cx.globalAlpha=a;cx.drawImage(bg,view.ox,view.oy,MAP*view.scale,MAP*view.scale);cx.globalAlpha=1;}
  else{cx.fillStyle="#0b0e13";cx.fillRect(0,0,w,h);}
  const lw=+document.getElementById("lineWidth").value,pr=+document.getElementById("pointSize").value;
  const sl=document.getElementById("showLines").checked,sp=document.getElementById("showPoints").checked,sm=document.getElementById("showMarks").checked;
  cx.lineJoin="round";cx.lineCap="round";
  for(const r of routes){
    if(!r.on)continue;
    cx.strokeStyle=r.color;cx.fillStyle=r.color;
    if(sl&&r.points.length>1){cx.lineWidth=lw;cx.beginPath();r.points.forEach((p,i)=>{const s=toS(p.x*K,p.y*K);i?cx.lineTo(s[0],s[1]):cx.moveTo(s[0],s[1]);});cx.stroke();}
    if(sp){cx.beginPath();r.points.forEach(p=>{const s=toS(p.x*K,p.y*K);cx.moveTo(s[0]+pr,s[1]);cx.arc(s[0],s[1],pr,0,Math.PI*2);});cx.fill();}
    if(sm&&r.points.length){
      const a0=toS(r.points[0].x*K,r.points[0].y*K);
      cx.fillStyle="#fff";cx.strokeStyle="#000";cx.lineWidth=Math.max(1.5,lw*.6);
      cx.beginPath();cx.arc(a0[0],a0[1],Math.max(5,pr*2.5),0,Math.PI*2);cx.fill();cx.stroke();
      const a1=toS(r.points[r.points.length-1].x*K,r.points[r.points.length-1].y*K);
      cx.fillStyle=r.color;
      cx.beginPath();cx.arc(a1[0],a1[1],Math.max(5,pr*2.5),0,Math.PI*2);cx.fill();cx.stroke();
      cx.fillStyle="#fff";cx.fillRect(a1[0]-2.5,a1[1]-2.5,5,5);
    }
  }
  cx.restore();
}
let drag=false,lx=0,ly=0;
cv.addEventListener("mousedown",e=>{drag=true;lx=e.clientX;ly=e.clientY;cv.classList.add("dragging");});
window.addEventListener("mousemove",e=>{
  if(drag){view.ox+=e.clientX-lx;view.oy+=e.clientY-ly;lx=e.clientX;ly=e.clientY;draw();}
  const rc=cv.getBoundingClientRect(),m=toM(e.clientX-rc.left,e.clientY-rc.top);
  if(m[0]>=0&&m[0]<=MAP&&m[1]>=0&&m[1]<=MAP){
    document.getElementById("coords").textContent="路线 "+Math.round(m[0]/K)+","+Math.round(m[1]/K)+" · 世界 "+Math.round(OX+m[0]/MAP*SIDE)+","+Math.round(OY+m[1]/MAP*SIDE);
  }else document.getElementById("coords").textContent="坐标 —";
});
window.addEventListener("mouseup",()=>{drag=false;cv.classList.remove("dragging");});
cv.addEventListener("wheel",e=>{
  e.preventDefault();
  const rc=cv.getBoundingClientRect(),mx=e.clientX-rc.left,my=e.clientY-rc.top;
  const f=e.deltaY<0?1.15:1/1.15,ns=Math.min(80,Math.max(.1,view.scale*f)),k=ns/view.scale;
  view.ox=mx-(mx-view.ox)*k;view.oy=my-(my-view.oy)*k;view.scale=ns;
  document.getElementById("zoom").textContent="缩放 "+Math.round(view.scale*100)+"%";draw();
});
cv.addEventListener("dblclick",()=>{fit();draw();});
for(const id of ["bgOpacity","lineWidth","pointSize","showLines","showPoints","showMarks"]){
  document.getElementById(id).addEventListener("input",draw);
  document.getElementById(id).addEventListener("change",draw);
}
document.getElementById("btnAll").addEventListener("click",()=>{routes.forEach(r=>r.on=true);render();draw();sel();});
document.getElementById("btnNone").addEventListener("click",()=>{routes.forEach(r=>r.on=false);render();draw();sel();});
window.addEventListener("resize",resize);
fit();resize();load();
})();

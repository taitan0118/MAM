const B='http://127.0.0.1:7071';let ck='';
async function api(m,p,b){const r=await fetch(B+p,{method:m,headers:{'Content-Type':'application/json','X-MAM':'1',cookie:ck},body:b?JSON.stringify(b):undefined});const c=r.headers.get('set-cookie');if(c)ck=c.split(';')[0];return r.json().catch(()=>({}))}
(async()=>{
 console.log(await api('POST','/api/login',{user:'admin',password:'Matkhau2026'}));
 for(const g of ['Tầng 1 · Kế toán','Tầng 2 · Kỹ thuật','Kho & Bán hàng'])console.log(await api('POST','/api/groups',{name:g}));
 const ms=await api('GET','/api/machines');
 const map={'KE-TOAN-02':'Tầng 1 · Kế toán','PC-KT-01':'Tầng 2 · Kỹ thuật','PC-HOT-03':'Tầng 2 · Kỹ thuật','PC-FULL-04':'Tầng 2 · Kỹ thuật','POS-KHO-01':'Kho & Bán hàng'};
 for(const m of ms)if(map[m.name])await api('PUT','/api/machines/'+m.id+'/group',{group:map[m.name]});
 console.log(await api('POST','/api/accounts',{name:'Lê Văn Kỹ',user:'ky.thuat',password:'Matkhau2026',role:'tech',scope:['Tầng 2 · Kỹ thuật']}));
 console.log(await api('POST','/api/accounts',{name:'Trần Thị Xem',user:'giam.sat',password:'Matkhau2026',role:'view',scope:[]}));
 const pc=ms.find(m=>m.name==='PC-KT-01');
 console.log(await api('POST','/api/machines/'+pc.id+'/command',{type:'message',args:{text:'Nhờ anh lưu file và đăng xuất trước 17h, máy sẽ bảo trì.'}}));
 console.log(await api('POST','/api/machines/'+pc.id+'/command',{type:'lock'}));
})();

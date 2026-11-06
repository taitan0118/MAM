const {chromium}=require('playwright');const {execSync}=require('child_process');
let fails=0;const ok=(c,m)=>{console.log((c?'PASS ':'FAIL ')+m);if(!c)fails++};
(async()=>{const b=await chromium.launch({executablePath:'/opt/pw-browsers/chromium'});
const login=async(p,u,pw)=>{await p.goto('http://127.0.0.1:7071/');await p.waitForTimeout(500);await p.fill('.loginbox input >> nth=0',u);await p.fill('.loginbox input >> nth=1',pw);await p.keyboard.press('Enter');await p.waitForTimeout(1200)};
// 1) kỹ thuật viên: đổi mật khẩu bắt buộc, phạm vi nhóm
let c=await b.newContext({locale:'vi-VN'});let p=await c.newPage();
await login(p,'ky.thuat','Matkhau2026');
ok(await p.locator('.forced').count()===1,'KTV: bắt buộc đổi mật khẩu lần đầu');
const f=p.locator('.forced input');await f.nth(0).fill('Matkhau2026');await f.nth(1).fill('KyThuat2026x');await f.nth(2).fill('KyThuat2026x');await p.getByRole('button',{name:'Lưu mật khẩu mới'}).click();await p.waitForTimeout(1500);
ok(await p.locator('#nav .navitem').count()>0&&!(await p.locator('.forced').count()),'KTV: vào được Console sau khi đổi');
const navs=await p.locator('#nav .navitem').allTextContents();console.log('   menu KTV:',navs.join(' | '));
const tab=await p.evaluate(async()=>{const m=await (await fetch('/api/machines')).json();const out=m.find(x=>x.group!=='Tầng 2 · Kỹ thuật');const inn=m.find(x=>x.group==='Tầng 2 · Kỹ thuật'&&x.online);
 const post=async(id,t)=>{const r=await fetch('/api/machines/'+id+'/command',{method:'POST',headers:{'X-MAM':'1','Content-Type':'application/json'},body:JSON.stringify({type:t})});return r.status};
 const grp=await fetch('/api/groups',{method:'POST',headers:{'X-MAM':'1','Content-Type':'application/json'},body:JSON.stringify({name:'Nhóm lậu'})});
 const acc=await fetch('/api/accounts');return {outCmd:out?await post(out.id,'lock'):'none',inCmd:inn?await post(inn.id,'lock'):'none',mk:grp.status,acc:acc.status}});
ok(tab.outCmd===403,'KTV: ra lệnh máy ngoài nhóm bị 403 ('+tab.outCmd+')');ok(tab.inCmd===200,'KTV: ra lệnh máy trong nhóm được ('+tab.inCmd+')');
ok(tab.mk===403,'KTV: không tạo được nhóm');ok(tab.acc===403,'KTV: không xem được tài khoản');
await c.close();
// 2) chỉ xem
c=await b.newContext({locale:'vi-VN'});p=await c.newPage();await login(p,'giam.sat','Matkhau2026');
const f2=p.locator('.forced input');await f2.nth(0).fill('Matkhau2026');await f2.nth(1).fill('XemThoi2026x');await f2.nth(2).fill('XemThoi2026x');await p.getByRole('button',{name:'Lưu mật khẩu mới'}).click();await p.waitForTimeout(1500);
await p.click('#nav .navitem >> nth=1');await p.waitForTimeout(500);
ok(await p.locator('thead input[type=checkbox]').count()===0,'Chỉ xem: không có ô chọn hàng loạt');
const v=await p.evaluate(async()=>{const m=await (await fetch('/api/machines')).json();const r=await fetch('/api/machines/'+m[0].id+'/command',{method:'POST',headers:{'X-MAM':'1','Content-Type':'application/json'},body:JSON.stringify({type:'lock'})});return r.status});
ok(v===403,'Chỉ xem: ra lệnh bị 403 ('+v+')');await c.close();
// 3) quên mật khẩu qua mam-server reset-admin (server đang chạy)
const out=execSync('/tmp/mamsrv -addr :7071 reset-admin giam.sat').toString();const m=out.match(/giam\.sat: (\S+)/);ok(!!m,'reset-admin in mật khẩu tạm');
c=await b.newContext({locale:'vi-VN'});p=await c.newPage();await login(p,'giam.sat',m[1]);ok(await p.locator('.forced').count()===1,'Sau reset: bị bắt đổi mật khẩu');
const err=await p.evaluate(async()=>{const r=await fetch('/api/machines');return r.status});ok(err===403,'Chưa đổi mật khẩu thì API bị chặn ('+err+')');await c.close();
// 4) đăng nhập sai khóa
c=await b.newContext({locale:'en-US'});p=await c.newPage();
for(let i=0;i<5;i++){await login(p,'admin','sai'+i)}
await login(p,'admin','Matkhau2026');const t=await p.locator('.loginbox').innerText();ok(/Too many|try again/i.test(t),'Khóa sau 5 lần sai, thông báo bằng tiếng Anh');await c.close();
console.log(fails?'FAILED '+fails:'ALL PASS');await b.close();process.exit(fails?1:0)})();

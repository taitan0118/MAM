const {chromium}=require('playwright');
(async()=>{const b=await chromium.launch({executablePath:'/opt/pw-browsers/chromium'});
for(const [L,loc] of [['vi','vi-VN'],['en','en-US'],['zh','zh-CN']]){
 const c=await b.newContext({locale:loc,viewport:{width:1280,height:780},deviceScaleFactor:1.5});const p=await c.newPage();
 const S=n=>p.screenshot({path:`shots/${L}-${n}.png`});
 // màn cài đặt lần đầu trên server trống
 await p.goto('http://127.0.0.1:7072/');await p.waitForTimeout(700);await S('setup');
 await p.goto('http://127.0.0.1:7071/');await p.waitForTimeout(600);await S('login');
 await p.click('.loginbox a');await p.waitForTimeout(400);await S('forgot');await p.keyboard.press('Escape');await p.waitForTimeout(300);
 await p.fill('.loginbox input >> nth=0','admin');await p.fill('.loginbox input >> nth=1','Matkhau2026');await p.keyboard.press('Enter');await p.waitForTimeout(1500);
 const names=['overview','machines','groups','alerts','accounts','audit','settings'];
 for(let i=0;i<7;i++){await p.click(`#nav .navitem >> nth=${i}`);await p.waitForTimeout(900);await S(names[i]);}
 await p.click('#nav .navitem >> nth=1');await p.waitForTimeout(500);
 await p.click('thead input[type=checkbox]');await p.waitForTimeout(300);await S('bulk');
 await p.getByRole('button').filter({hasText:/Gửi tin nhắn|Send message|发送消息/}).first().click();await p.waitForTimeout(400);await p.fill('textarea','Nhờ anh lưu file trước 17h, máy sẽ bảo trì.');await S('message');await p.keyboard.press('Escape');await p.waitForTimeout(300);
 await p.click('tbody tr:has-text("PC-HOT-03")');await p.waitForTimeout(1500);await S('detail');
 await p.click('.tabs button >> nth=1');await p.waitForTimeout(400);await S('hardware');
 await p.click('.tabs button >> nth=3');await p.waitForTimeout(400);await S('commands');
 await c.close();}
await b.close();})();

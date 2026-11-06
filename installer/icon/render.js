const {chromium}=require('playwright');const fs=require('fs');
(async()=>{const b=await chromium.launch({executablePath:'/opt/pw-browsers/chromium'});
const p=await b.newPage({viewport:{width:1024,height:1024}});
for(const n of ['mam-server','mam-agent']){await p.setContent('<body style="margin:0;background:transparent">'+fs.readFileSync(n+'.svg','utf8')+'</body>');
await p.screenshot({path:n+'-1024.png',omitBackground:true});}await b.close();})();

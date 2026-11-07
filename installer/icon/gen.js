// Sinh biểu tượng MAM (logo phương án B) từ chính missing-intro.js: node gen.js
const fs = require('fs'), path = require('path');
global.window = global; global.speechSynthesis = null;
eval(fs.readFileSync(path.join(__dirname, '../../server/web/missing-intro.js'), 'utf8'));
const L = global.MissingLogo, w = (f, s) => fs.writeFileSync(path.join(__dirname, f), s);
w('mam-server.svg', L.svg({ size: 1024, tile: true, badge: 'server' }));
w('mam-agent.svg', L.svg({ size: 1024, tile: true, badge: 'agent' }));
const web = path.join(__dirname, '../../server/web/');
fs.writeFileSync(web + 'favicon.svg', L.svg({ size: 64, tile: true, badge: 'server' }));
fs.writeFileSync(web + 'logo-light.svg', L.svg({ size: 200, theme: 'light', badge: 'server' }));
fs.writeFileSync(web + 'logo-dark.svg', L.svg({ size: 200, theme: 'dark', badge: 'server' }));
fs.writeFileSync(path.join(__dirname, '../setup-server/logo.svg'), L.svg({ size: 56, tile: true, badge: 'server' }));
fs.writeFileSync(path.join(__dirname, '../setup-agent/logo.svg'), L.svg({ size: 56, tile: true, badge: 'agent' }));
fs.writeFileSync(path.join(__dirname, '../uikit/logo.svg'), L.svg({ size: 56, tile: true, badge: 'server' }));
// logo nhúng thẳng trong HTML (cửa sổ agent, màn chọn server của Console): thay đúng thẻ svg cũ, giữ cỡ
for (const f of ['../../agent/ui.html', '../../console/connect.html']) {
  const fp = path.join(__dirname, f);
  fs.writeFileSync(fp, fs.readFileSync(fp, 'utf8').replace(/<svg xmlns="http:\/\/www\.w3\.org\/2000\/svg" viewBox="0 0 200 200" width="(\d+)" height="\d+" role="img" aria-label="MAM (Server|Agent)">.*?<\/svg>/g,
    (m, size, kind) => L.svg({ size: +size, tile: true, badge: kind.toLowerCase() })));
}

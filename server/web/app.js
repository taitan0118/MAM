/* MAM Console — giao diện web nói chuyện với mam-server qua /api. Không dùng thư viện ngoài. */
(function () {
'use strict';
const $ = (s, r) => (r || document).querySelector(s);
const t = MAMI.t, trErr = MAMI.t;   // t(): dịch theo ngôn ngữ đang chọn (xem i18n.js)

/* ---------- tiện ích ---------- */
function el(tag, attrs, ...kids) {
  const e = document.createElement(tag);
  for (const k in (attrs || {})) {
    const v = attrs[k];
    if (v == null || v === false) continue;
    if (k === 'class') e.className = v;
    else if (k === 'html') e.innerHTML = v;      // chỉ dùng cho icon tĩnh, không bao giờ cho dữ liệu từ server
    else if (k.startsWith('on')) e.addEventListener(k.slice(2), v);
    else e.setAttribute(k, v === true ? '' : (k === 'placeholder' || k === 'title' || k === 'aria-label') ? t(v) : v);
  }
  for (const c of kids.flat()) if (c != null && c !== false) e.append(c.nodeType ? c : document.createTextNode(typeof c === 'string' ? t(c) : String(c)));
  return e;
}
const ico = p => el('span', { html: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" width="18" height="18">' + p + '</svg>', style: 'display:inline-flex' });
const ICONS = {
  overview: '<rect x="3" y="3" width="7" height="9" rx="1.5"/><rect x="14" y="3" width="7" height="5" rx="1.5"/><rect x="14" y="12" width="7" height="9" rx="1.5"/><rect x="3" y="16" width="7" height="5" rx="1.5"/>',
  machines: '<rect x="3" y="4" width="18" height="12" rx="2"/><path d="M8 20h8M12 16v4"/>',
  groups: '<path d="M3 7l9-4 9 4-9 4zM3 12l9 4 9-4M3 17l9 4 9-4"/>',
  alerts: '<path d="M12 3l10 18H2zM12 10v5M12 18h.01"/>',
  chat: '<path d="M4 5h16v10H9l-5 4zM8 9h8M8 12h5"/>',
  requests: '<path d="M21 15a2 2 0 01-2 2H7l-4 4V5a2 2 0 012-2h14a2 2 0 012 2z"/>',
  inventory: '<rect x="4" y="3" width="16" height="18" rx="2"/><path d="M8 8h8M8 12h8M8 16h5"/>',
  policy: '<path d="M12 3l8 3v6c0 5-3.5 8-8 9-4.5-1-8-4-8-9V6z"/>',
  security: '<path d="M12 3l8 3v6c0 5-3.5 8-8 9-4.5-1-8-4-8-9V6z"/><path d="M9 12l2 2 4-4"/>',
  discover: '<circle cx="11" cy="11" r="7"/><path d="M21 21l-4.3-4.3"/>',
  net: '<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a14 14 0 010 18M12 3a14 14 0 000 18"/>',
  report: '<path d="M4 20V10M11 20V4M18 20v-7"/>',
  uptime: '<path d="M12 3v8"/><path d="M7 6.3a8 8 0 1010 0"/>',
  accounts: '<circle cx="12" cy="8" r="4"/><path d="M4 21c0-4 4-6 8-6s8 2 8 6"/>',
  audit: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
  settings: '<circle cx="12" cy="12" r="3"/><path d="M19 12a7 7 0 00-.1-1.2l2-1.5-2-3.5-2.3 1a7 7 0 00-2-1.2L14 3h-4l-.6 2.6a7 7 0 00-2 1.2l-2.3-1-2 3.5 2 1.5a7 7 0 000 2.4l-2 1.5 2 3.5 2.3-1a7 7 0 002 1.2L10 21h4l.6-2.6a7 7 0 002-1.2l2.3 1 2-3.5-2-1.5c.1-.4.1-.8.1-1.2z"/>'
};
const ROLE = { super: 'Super admin', tech: 'Kỹ thuật viên', view: 'Chỉ xem' };
const pad = n => String(n).padStart(2, '0');
const fmtDT = x => { const d = new Date(x); return isNaN(d) || d.getFullYear() < 2000 ? '—' : pad(d.getDate()) + '/' + pad(d.getMonth() + 1) + '/' + d.getFullYear() + ' ' + pad(d.getHours()) + ':' + pad(d.getMinutes()); };
function ago(x) {
  const d = new Date(x), s = (Date.now() - d) / 1000;
  if (isNaN(d) || d.getFullYear() < 2000) return '—';
  if (s < 60) return t('vừa xong');
  if (s < 3600) return t('{0} phút trước', Math.floor(s / 60));
  if (s < 86400) return t('{0} giờ trước', Math.floor(s / 3600));
  return t('{0} ngày trước', Math.floor(s / 86400));
}
function uptime(s) { if (s == null) return '—'; const d = Math.floor(s / 86400), h = Math.floor(s % 86400 / 3600), m = Math.floor(s % 3600 / 60); return d ? t('{0} ngày {1} giờ', d, h) : h ? t('{0} giờ {1} phút', h, m) : t('{0} phút', m); }
const num = (v, d = 0) => v == null ? '—' : Number(v).toFixed(d);
const col = (v, warn, crit) => v >= crit ? 'var(--crit)' : v >= warn ? 'var(--warn)' : 'var(--good)';

let me = null, settings = null, groups = [];
const route = { view: 'overview', id: null, tab: 'live' };
let timer = null, ctrl = null;

async function api(method, path, body) {
  const o = { method, headers: {}, credentials: 'same-origin' };
  if (method !== 'GET') o.headers['X-MAM'] = '1';
  if (body !== undefined) { o.headers['Content-Type'] = 'application/json'; o.body = JSON.stringify(body); }
  let r;
  try { r = await fetch(path, o); } catch (e) { throw new Error('Không liên lạc được server'); }
  let j = null; try { j = await r.json(); } catch (e) { }
  if (r.status === 401 && path !== '/api/login') { showAuth(); throw new Error('Phiên đăng nhập đã hết, hãy đăng nhập lại'); }
  if (r.status === 403 && j && /đổi mật khẩu/.test(j.error || '') && me) { forceChange(); throw new Error(j.error); }
  if (!r.ok) throw new Error(trErr((j && j.error) || t('Lỗi {0}', r.status)));
  return j;
}
function toast(msg, kind) {
  const n = el('div', { class: 'toast ' + (kind || '') }, msg);
  $('#toasts').append(n);
  requestAnimationFrame(() => n.classList.add('in'));
  setTimeout(() => { n.classList.remove('in'); setTimeout(() => n.remove(), 250); }, kind === 'warn' || kind === 'crit' ? 6000 : 3500);
}
const fail = e => toast(e.message, 'warn');

function modal(title, body, actions, big) {
  const back = el('div', { class: 'modal-back' });
  const esc = e => { if (e.key === 'Escape') close(); };
  const close = () => { back.classList.remove('in'); setTimeout(() => back.remove(), 200); document.removeEventListener('keydown', esc); };
  const foot = el('div', { class: 'modal-f' });
  actions.forEach(a => foot.append(el('button', { class: 'btn ' + (a.cls || ''), onclick: () => a.on(close) }, a.label)));
  back.append(el('div', { class: 'modal' + (big ? ' modal-big' : ''), role: 'dialog', 'aria-modal': 'true' },
    el('div', { class: 'modal-h' }, el('h3', {}, title), el('button', { class: 'iconbtn', 'aria-label': 'Đóng', onclick: close, html: '<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 6L6 18M6 6l12 12"/></svg>' })),
    el('div', { class: 'modal-b' }, body), foot));
  back.addEventListener('click', e => { if (e.target === back) close(); });
  document.addEventListener('keydown', esc);
  document.body.append(back);
  requestAnimationFrame(() => back.classList.add('in'));
  const f = back.querySelector('input,select,textarea'); if (f) setTimeout(() => f.focus(), 60);
  return close;
}
function confirmBox(title, msg, label, danger, fn) {
  modal(title, el('p', { style: 'margin:0;color:var(--muted);font-size:13.5px;line-height:1.55' }, msg),
    [{ label: 'Hủy', on: c => c() }, { label, cls: danger ? 'danger' : 'primary', on: c => { c(); fn(); } }]);
}
function field(label, input, hint) { return el('div', { class: 'field' }, el('label', { class: 'flbl' }, label), input, el('div', { class: 'ferr' }), hint ? el('div', { class: 'fhint' }, hint) : null); }
function setErr(f, m) { f.querySelector('.ferr').textContent = m ? t(m) : ''; const i = f.querySelector('.input'); if (i) i.classList.toggle('bad', !!m); return !m; }
const input = (ph, type, val) => el('input', { class: 'input', placeholder: ph || '', type: type || 'text', value: val || '', autocomplete: type === 'password' ? 'new-password' : 'off' });
function select(opts, val, onchange) {
  const s = el('select', { class: 'input', onchange: onchange ? e => onchange(e.target.value) : null });
  opts.forEach(([v, l]) => s.append(el('option', { value: v, ...(v === val ? { selected: true } : {}) }, l)));
  return s;
}
const infoNote = x => el('div', { class: 'note' }, el('span', { html: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="9"/><path d="M12 8h.01M11 12h1v5h1"/></svg>' }), el('div', {}, x));
const pill = (cls, x) => el('span', { class: 'pill ' + cls }, el('span', { class: 'd' }), x);
const card = (title, sub, body, right) => el('div', { class: 'card' }, el('div', { class: 'card-h' }, el('h3', {}, title), sub ? el('span', { class: 'sub' }, '· ' + t(sub)) : null, el('div', { style: 'flex:1' }), right), el('div', { class: 'card-b' }, body));
const kpi = (label, color, val, sub, fill) => el('div', { class: 'kpi' }, el('div', { class: 'lbl' }, el('span', { class: 'dot', style: 'background:' + color }), label), el('div', { class: 'val', style: 'color:' + color }, val), el('div', { class: 'sub' }, sub || ''), fill != null ? el('div', { class: 'kfill' }, el('i', { style: 'width:' + Math.max(0, Math.min(100, fill)) + '%;background:' + color })) : null);
const canWrite = () => me && me.role !== 'view';
const isSuper = () => me && me.role === 'super';
function bar(v, warn, crit) { return el('div', { class: 'metric' }, el('div', { class: 'bar' }, el('i', { style: 'width:' + Math.min(100, v || 0) + '%;background:' + col(v, warn, crit) })), el('span', { class: 'mono' }, num(v) + '%')); }
function table(heads, rows, empty) {
  const tb = el('table', {}, el('thead', {}, el('tr', {}, heads.map(h => el('th', { class: h.cls || '' }, h.t || h)))), el('tbody', {}, rows));
  if (!rows.length) return el('div', { class: 'empty' }, empty || 'Chưa có dữ liệu');
  return el('div', { class: 'tblwrap' }, tb); // bảng dài thì cuộn trong khung, tiêu đề dính trên
}

/* ---------- đăng nhập, cài đặt lần đầu, đổi mật khẩu ---------- */
async function showAuth() {
  clearInterval(timer); me = null; $('#app').hidden = true;
  let st = { setup: false };
  try { st = await (await fetch('/api/status')).json(); } catch (e) { }
  document.querySelectorAll('.loginwrap').forEach(x => x.remove());
  const wrap = el('div', { class: 'loginwrap' });
  const logo = el('span', { style: 'display:block;width:34px;height:34px' }); logo.innerHTML = MissingLogo.svg({ size: 34, theme: isDark() ? 'dark' : 'light', badge: 'server', ring: 'var(--surface)' });
  const head = el('div', { style: 'display:flex;align-items:center;gap:10px;justify-content:center' }, logo, el('div', {}, el('b', { style: 'font-size:15px' }, 'MAM Console'), el('div', { style: 'font-size:11px;color:var(--faint)' }, st.setup ? 'Cài đặt lần đầu' : 'Đăng nhập hệ thống')));
  const box = el('div', { class: 'loginbox' }, head);
  wrap.append(box, el('div', { class: 'langfloat' }, MAMI.picker())); document.body.append(wrap);
  if (st.setup) {
    const nm = input('Họ tên người quản trị'), un = input('vd admin.congty'), p1 = input('Ít nhất 8 ký tự, có chữ và số', 'password'), p2 = input('Nhập lại mật khẩu', 'password');
    const fn = field('Họ tên', nm), fu = field('Tài khoản đăng nhập', un, 'Chữ thường, số, dấu . _'), f1 = field('Mật khẩu', p1, 'Ghi lại ở nơi an toàn. Quên mật khẩu thì phải ngồi tại máy server để khôi phục'), f2 = field('Nhập lại mật khẩu', p2);
    const go = async () => {
      let ok = true;
      if (!setErr(fn, nm.value.trim().length >= 2 ? '' : 'Nhập họ tên')) ok = false;
      if (!setErr(fu, /^[a-z0-9._]{3,32}$/.test(un.value.trim()) ? '' : 'Tài khoản 3 đến 32 ký tự: chữ thường, số, dấu . _')) ok = false;
      if (!setErr(f1, p1.value.length < 8 ? 'Mật khẩu ít nhất 8 ký tự' : !/[A-Za-z]/.test(p1.value) || !/\d/.test(p1.value) ? 'Cần có cả chữ và số' : '')) ok = false;
      if (!setErr(f2, p1.value === p2.value ? '' : 'Hai mật khẩu chưa giống nhau')) ok = false;
      if (!ok) return;
      try { const r = await api('POST', '/api/setup', { name: nm.value.trim(), user: un.value.trim(), password: p1.value }); me = r.me; wrap.remove(); enter(); toast('Đã tạo Super admin đầu tiên. Nên tạo thêm một Super admin dự phòng.'); } catch (e) { setErr(f2, e.message); }
    };
    box.append(el('div', { style: 'font-size:12.5px;color:var(--muted);line-height:1.55' }, 'Lần đầu cài phần mềm trên máy server: tạo tài khoản Super admin đầu tiên. Không có mật khẩu mặc định.'), fn, fu, f1, f2, el('button', { class: 'btn primary', style: 'width:100%;justify-content:center', onclick: go }, 'Tạo tài khoản và vào Console'));
    wrap.addEventListener('keydown', e => { if (e.key === 'Enter') go(); });
    return;
  }
  const un = input('Tên đăng nhập'), pw = input('Mật khẩu', 'password');
  pw.autocomplete = 'current-password';
  const code = input('6 số trong ứng dụng xác thực'); code.inputMode = 'numeric'; code.maxLength = 7; code.autocomplete = 'one-time-code';
  const fu = field('Tên đăng nhập', un), fp = field('Mật khẩu', pw), fc = field('Mã xác thực hai bước', code); fc.hidden = true;
  const go = async () => {
    if (!setErr(fu, un.value.trim() ? '' : 'Nhập tên đăng nhập')) return;
    if (!setErr(fp, pw.value ? '' : 'Nhập mật khẩu')) return;
    try { const r = await api('POST', '/api/login', { user: un.value.trim(), password: pw.value, code: code.value }); me = r.me; wrap.remove(); enter(); }
    catch (e) { if (e.message === '2FA_REQUIRED') { fc.hidden = false; code.focus(); } else setErr(fc.hidden ? fp : fc, e.message); }
  };
  box.append(fu, fp, fc, el('button', { class: 'btn primary', style: 'width:100%;justify-content:center', onclick: go }, 'Đăng nhập'),
    el('a', { onclick: forgotInfo }, 'Quên mật khẩu?'));
  wrap.addEventListener('keydown', e => { if (e.key === 'Enter') go(); });
  setTimeout(() => un.focus(), 50);
}
function forgotInfo() {
  modal('Quên mật khẩu', el('div', { style: 'font-size:13px;line-height:1.65;color:var(--muted)' },
    el('p', { style: 'margin:0 0 10px' }, 'Nhờ một Super admin khác mở mục Tài khoản, chọn dòng của bạn và bấm "Đặt lại mật khẩu". Họ sẽ đưa bạn một mật khẩu tạm, bạn đăng nhập và hệ thống bắt đặt mật khẩu mới.'),
    el('p', { style: 'margin:0 0 6px' }, 'Nếu bạn là Super admin duy nhất, hãy ngồi tại máy server, mở cửa sổ lệnh (Administrator) rồi chạy:'),
    el('div', { class: 'pre' }, 'mam-server reset-admin <tài khoản>'),
    el('p', { style: 'margin:10px 0 0' }, 'Lệnh in ra mật khẩu tạm. Mọi lần đặt lại đều được ghi vào Nhật ký thao tác.')),
    [{ label: 'Đã hiểu', cls: 'primary', on: c => c() }]);
}
function forceChange() {
  if ($('.forced')) return;
  const np = input('', 'password'), cp = input('', 'password'), cur = input('', 'password');
  const f0 = field('Mật khẩu tạm hiện tại', cur), f1 = field('Mật khẩu mới', np, 'Ít nhất 8 ký tự, có chữ và số'), f2 = field('Nhập lại mật khẩu mới', cp);
  const close = modal('Bắt buộc đổi mật khẩu', el('div', { class: 'forced', style: 'display:flex;flex-direction:column;gap:12px' }, el('div', { style: 'font-size:12.5px;color:var(--muted);line-height:1.55' }, 'Bạn đang dùng mật khẩu tạm hoặc mật khẩu do quản trị viên đặt. Hãy đặt mật khẩu mới để tiếp tục.'), f0, f1, f2),
    [{ label: 'Đăng xuất', on: async () => { try { await api('POST', '/api/logout', {}); } catch (e) { } location.reload(); } },
     { label: 'Lưu mật khẩu mới', cls: 'primary', on: async c => {
        const a = setErr(f1, np.value.length < 8 ? 'Ít nhất 8 ký tự' : !/[A-Za-z]/.test(np.value) || !/\d/.test(np.value) ? 'Cần có cả chữ và số' : np.value === cur.value ? 'Phải khác mật khẩu cũ' : '');
        const b = setErr(f2, cp.value === np.value ? '' : 'Hai mật khẩu không khớp');
        if (!(a && b)) return;
        try { await api('POST', '/api/me/password', { current: cur.value, new: np.value }); me.mustChange = false; c(); toast('Đã đổi mật khẩu'); render(); } catch (e) { setErr(f0, e.message); }
      } }]);
  // không cho đóng bằng Esc, bấm ra ngoài hoặc nút X
  const back = $('.modal-back:last-of-type');
  back.querySelector('.modal-h .iconbtn').style.display = 'none';
  window.addEventListener('keydown', e => { if (e.key === 'Escape' && me && me.mustChange) e.stopImmediatePropagation(); }, true);
  back.addEventListener('click', e => { if (e.target === back) e.stopImmediatePropagation(); }, true);
}

/* ---------- khung ứng dụng ---------- */
const NAV = [['overview', 'Tổng quan'], ['alerts', 'Cảnh báo'], ['requests', 'Yêu cầu hỗ trợ'], ['chat', 'Chat với máy'], ['machines', 'Máy'], ['groups', 'Nhóm máy'], ['policy', 'Chính sách'], ['security', 'Bảo mật & bản vá'], ['discover', 'Phát hiện máy'], ['net', 'Qua Internet'], ['inventory', 'Kiểm kê'], ['report', 'Báo cáo'], ['uptime', 'Thời gian hoạt động'], ['accounts', 'Tài khoản'], ['audit', 'Nhật ký thao tác'], ['settings', 'Cài đặt']];
const TITLES = {
  overview: ['Tổng quan', 'Toàn hệ thống · cập nhật trực tiếp'], machines: ['Máy', 'Danh sách, lọc, ra lệnh'], detail: ['Chi tiết máy', ''],
  groups: ['Nhóm máy', 'Sắp xếp máy theo khu vực hoặc phòng ban'], alerts: ['Cảnh báo', 'Nhiệt độ, CPU, ổ cứng, mất kết nối'],
  chat: ['Chat với máy', 'Nhắn hai chiều với người ngồi máy, hiện ngay trong cửa sổ MAM'], requests: ['Yêu cầu hỗ trợ', 'Người ngồi máy gửi lên từ cửa sổ MAM'], policy: ['Chính sách', 'USB, chặn web/ứng dụng, giờ dùng máy theo nhóm'],
  inventory: ['Kiểm kê', 'Phần cứng và bản quyền phần mềm từng máy'], security: ['Bảo mật & bản vá', 'Diệt virus và Windows Update — chỉ xem'],
  discover: ['Phát hiện máy', 'Quét mạng LAN tìm máy chưa cài agent'], net: ['Qua Internet', 'Dùng Console khi không ở trong mạng công ty'],
  report: ['Báo cáo', 'Tổng hợp theo khoảng thời gian, xuất CSV'], uptime: ['Thời gian hoạt động', 'Máy bật lúc nào, tắt lúc nào, tổng bao nhiêu giờ'],
  accounts: ['Tài khoản', 'Ai được vào Console'], audit: ['Nhật ký thao tác', 'Ai làm gì, lúc nào, từ máy nào'], settings: ['Cài đặt', 'Ngưỡng cảnh báo, thông báo, sao lưu, cách cài agent']
};
const isDark = () => document.documentElement.dataset.theme === 'dark' || (!document.documentElement.dataset.theme && matchMedia('(prefers-color-scheme: dark)').matches);
function applyTheme(x) {
  if (x) document.documentElement.dataset.theme = x; else delete document.documentElement.dataset.theme;
  $('#brandLogo').innerHTML = MissingLogo.svg({ size: 30, theme: isDark() ? 'dark' : 'light', badge: 'server', ring: 'var(--surface)' });
}
function go(v, id) { route.view = v; if (id) route.id = id; if (v === 'detail') route.tab = 'live'; $('#side').classList.remove('open'); $('#scrim').classList.remove('on'); renderNav(); render(); scrollTo(0, 0); }
let badges = { alerts: 0, requests: 0, chat: 0 };
// Số đỏ cạnh mục menu: cảnh báo đang mở và yêu cầu hỗ trợ mới (chưa ai nhận xử lý).
async function refreshBadges() {
  try {
    const [o, q, c] = await Promise.all([api('GET', '/api/overview'), api('GET', '/api/requests?status=new'), me.role === 'view' ? { unread: 0 } : api('GET', '/api/chat?unread=1')]);
    const n = { alerts: o.openAlerts || 0, requests: q.length, chat: c.unread || 0 };
    if (n.alerts !== badges.alerts || n.requests !== badges.requests || n.chat !== badges.chat) { badges = n; renderNav(); }
  } catch (e) { }
}
function renderNav() {
  const nav = $('#nav'); nav.innerHTML = '';
  NAV.forEach(([k, l]) => nav.append(el('button', { class: 'navitem' + (route.view === k || (k === 'machines' && route.view === 'detail') ? ' on' : ''), onclick: () => go(k) }, ico(ICONS[k]), el('span', {}, l), badges[k] ? el('span', { class: 'count badge-red' }, badges[k] > 99 ? '99+' : String(badges[k])) : null)));
}
function setWho() {
  const w = me.name.trim().split(/\s+/);
  const av = $('#avatar'); av.textContent = ((w[0][0] || '') + (w.length > 1 ? w[w.length - 1][0] : '')).toUpperCase(); av.style.backgroundImage = me.av ? 'url(/api/avatar/u/' + me.id + '?v=' + me.av + ')' : ''; av.style.backgroundSize = 'cover'; if (me.av) av.textContent = '';
  $('#whoName').textContent = me.name; $('#whoRole').textContent = t(ROLE[me.role] || '');
}
async function enter() {
  $('#app').hidden = false; setWho();
  try { settings = await api('GET', '/api/settings'); const g = await api('GET', '/api/groups'); groups = g.groups.map(x => x.name); } catch (e) { }
  renderNav(); render(); refreshBadges();
  if (!enter.played) { enter.played = 1; applyTheme(document.documentElement.dataset.theme); MissingLogo.intro({ target: '#brandLogo', word: '#site-word', theme: isDark() ? 'dark' : 'light', badge: 'server', ring: getComputedStyle(document.documentElement).getPropertyValue('--surface').trim() || undefined }); }
  clearInterval(timer); timer = setInterval(tick, 4000);
  if (me.mustChange) forceChange();
}
function tick() {
  if (me && !document.hidden) refreshBadges();
  if (document.hidden || $('.modal-back') || !me || me.mustChange) return;
  // tab Tiến trình/Ổ cứng nạp dữ liệu theo yêu cầu: làm mới tự động sẽ xóa kết quả vừa lấy
  if (route.view === 'chat' && (document.activeElement || {}).tagName === 'TEXTAREA' && document.activeElement.value) return;
  if (route.view === 'chat') { render(true); return; }
  if (route.view === 'detail' && (route.tab === 'procs' || route.tab === 'disk' || route.tab === 'up' || route.tab === 'err')) return;
  if (route.view === 'overview' || route.view === 'machines' || route.view === 'detail' || route.view === 'alerts') render(true);
}
let rendering = 0;
async function render(quiet) {
  const my = ++rendering, v = $('#view');
  const tt = route.view === 'detail' ? ['Chi tiết máy', ''] : TITLES[route.view];
  $('#pgTitle').textContent = t(tt[0]); $('#pgSub').textContent = t(tt[1]);
  const fresh = el('div', {});
  try { await VIEWS[route.view](fresh, quiet); } catch (e) { if (!quiet) fail(e); return; }
  if (my !== rendering) return;
  // giữ ô tìm kiếm và vị trí cuộn khi làm mới tự động
  const sc = scrollY, act = document.activeElement, keep = act && act.id ? act.id : null, selS = act && act.selectionStart;
  const SB = '.scrollbox,.tblwrap,.chat-log,.ov .card-b', keepTop = quiet ? [...v.querySelectorAll(SB)].map(n => n.scrollTop) : [];
  v.replaceChildren(...fresh.childNodes);
  if (quiet) { scrollTo(0, sc); v.querySelectorAll(SB).forEach((n, i) => { if (keepTop[i] != null && !(n.classList.contains('chat-log') && !chatKeep)) n.scrollTop = keepTop[i]; }); chatKeep = null; }
  if (keep) { const n = document.getElementById(keep); if (n) { n.focus(); try { n.setSelectionRange(selS, selS); } catch (e) { } } }
}

/* ---------- Tổng quan ---------- */
const VIEWS = {};
const OVF = { f: 'all', r: 'all' };
VIEWS.overview = async v => {
  const noChat = me.role === 'view';
  const [o, ml, rq, ch] = await Promise.all([api('GET', '/api/overview'), api('GET', '/api/machines'), api('GET', '/api/requests?status='), noChat ? { convs: [], unread: 0 } : api('GET', '/api/chat')]);
  const lim = { t: settings ? settings.temp : 85, d: settings ? settings.disk : 10 };
  const level = m => { const hot = m.online && m.temp != null && m.temp >= lim.t, low = m.diskFree != null && m.diskFree <= lim.d;
    return !m.online ? 'off' : (hot && m.temp >= lim.t + 5) || low ? 'crit' : hot || (m.cpu != null && m.cpu >= 90) ? 'warn' : 'ok'; };
  const items = ml.map(m => ({ m, lvl: level(m) })).sort((a, b) => ({ crit: 0, warn: 1, ok: 2, off: 3 }[a.lvl] - { crit: 0, warn: 1, ok: 2, off: 3 }[b.lvl]));
  const n = k => items.filter(x => x.lvl === k).length;
  const ovk = [['Tổng máy', 'var(--accent)', o.total, 'đã cài agent'], ['Đang online', 'var(--good)', o.online, o.total ? t('{0}% số máy', Math.round(o.online / o.total * 100)) : '—'],
    ['Cảnh báo mở', 'var(--warn)', o.openAlerts, 'cần xử lý'], ['Máy quá nhiệt', 'var(--crit)', o.hot, t('vượt ngưỡng {0}°C', lim.t)]]
    .map(([l, c, val, sub]) => el('div', { class: 'ovk-i' }, el('b', { style: 'color:' + c }, String(val)), el('div', {}, el('span', {}, l), el('small', {}, sub))));
  const kpiRow = el('div', { class: 'ovk' }, ovk); // số liệu tổng nằm đầu thẻ Sức khỏe (bố cục: 1 khối dọc bên trái + 4 thẻ 2×2 bên phải)
  const emptyNote = !o.total ? el('div', {}, infoNote('Chưa có máy nào. Cài MAM Agent trên máy client (xem mục Cài đặt) và máy sẽ tự hiện ở đây sau vài giây, không cần đăng nhập hay duyệt.')) : null;
  const mb = (m, l, val, c) => el('div', {}, el('span', {}, l, el('b', {}, m.online && val != null ? num(val) + (l === 'Nhiệt' ? '°C' : '%') : '--')), el('div', { class: 'bar', style: 'max-width:none;height:5px' }, el('i', { style: 'width:' + Math.min(100, m.online && val != null ? val : 0) + '%;background:' + c })));
  const L = { ok: 'Ổn', warn: 'Cần chú ý', crit: 'Nghiêm trọng', off: 'Offline' }, DOT = { ok: 'var(--good)', warn: 'var(--warn)', crit: 'var(--crit)', off: 'var(--faint)' };
  const shown = items.filter(x => OVF.f === 'all' || x.lvl === OVF.f);
  const tiles = shown.map(({ m, lvl }) => el('div', { class: 'tile ' + lvl, onclick: () => go('detail', m.id) },
    el('div', { class: 'th' }, el('span', { class: 'dot', style: 'background:' + DOT[lvl] }), el('div', { style: 'min-width:0;flex:1' }, el('b', {}, m.name), el('small', {}, m.group || 'Chưa phân nhóm')), pill(lvl === 'crit' ? 'crit' : lvl === 'ok' ? 'ok' : lvl === 'off' ? 'off' : 'warn', L[lvl])),
    el('div', { class: 'ms' }, mb(m, 'CPU', m.cpu, 'var(--accent)'), mb(m, 'RAM', m.ram, '#6a5ae0'), mb(m, 'Nhiệt', m.temp, col(m.temp, 70, lim.t)))));
  const chip = (k, l, c, cnt) => el('button', { class: 'ovchip' + (OVF.f === k ? ' on' : ''), onclick: () => { OVF.f = k; render(); } }, c ? el('i', { style: 'background:' + c }) : null, l, el('b', {}, String(cnt)));
  const health = el('div', { class: 'card' }, el('div', { class: 'card-h', style: 'flex-wrap:wrap' }, el('h3', {}, 'Sức khỏe từng máy'), el('div', { style: 'flex:1' }),
    el('div', { class: 'ovchips' }, chip('all', 'Tất cả', null, items.length), chip('crit', 'Nghiêm trọng', 'var(--crit)', n('crit')), chip('warn', 'Cần chú ý', 'var(--warn)', n('warn')), chip('off', 'Offline', 'var(--faint)', n('off')))),
    el('div', { class: 'card-b' }, el('div', { class: 'tiles scrollbox' }, tiles.length ? tiles : el('div', { class: 'empty' }, ml.length ? 'Không có máy nào ở mức này' : 'Chưa có máy')),
      ''));
  const rowCss = 'display:flex;gap:11px;align-items:flex-start;padding:9px 0;border-top:1px solid var(--border);cursor:pointer';
  const alerts = card('Cảnh báo gần đây', null, el('div', { class: 'scrollbox short' }, (o.recent || []).length ? o.recent.map(a => el('div', { style: rowCss, onclick: () => a.machine === '__server' ? go('settings') : go('detail', a.machine) },
    pill(a.sev === 'crit' ? 'crit' : 'warn', a.sev === 'crit' ? 'Nghiêm trọng' : 'Cảnh báo'), el('div', { style: 'flex:1;min-width:0' }, el('b', {}, a.name), el('div', { style: 'font-size:12.5px;color:var(--muted)' }, a.msg)), el('span', { style: 'font-size:12px;color:var(--faint)' }, ago(a.opened)))) : el('div', { class: 'empty' }, 'Không có cảnh báo đang mở')),
    el('button', { class: 'btn', style: 'padding:2px 9px;font-size:12px', onclick: () => go('alerts') }, 'Xem tất cả'));
  const cnt = s => rq.filter(q => q.status === s).length;
  const ord = { new: 0, working: 1, done: 2 }, SL = { new: ['new', 'Mới'], working: ['work', 'Đang xử lý'], done: ['done', 'Xong'] };
  const open = rq.filter(q => OVF.r === 'all' || q.status === OVF.r).sort((a, b) => (a.status !== 'done' && !!b.urgent) - (b.status !== 'done' && !!a.urgent) || ord[a.status] - ord[b.status] || String(b.created).localeCompare(String(a.created)));
  const rcnt = (k, l, c) => el('button', { class: 'ovp ' + c + (OVF.r === k ? ' on' : ''), onclick: () => { OVF.r = k; render(); } }, el('b', {}, String(k === 'all' ? rq.length : cnt(k))), l);
  const reqCard = card('Yêu cầu hỗ trợ', null, el('div', { class: 'scrollbox short' }, open.length ? open.slice(0, 60).map(q => el('div', { style: rowCss, onclick: () => go('requests') },
    el('span', { class: 'ovp ' + SL[q.status][0], style: 'flex:none;margin-top:1px' }, SL[q.status][1]),
    el('div', { style: 'flex:1;min-width:0;font-size:13px' }, q.urgent && q.status !== 'done' ? el('b', { style: 'color:var(--crit)' }, t('Gấp') + ' · ') : null, el('b', {}, q.name), el('span', { style: 'color:var(--muted)' }, ' · ' + q.text)), el('span', { style: 'font-size:12px;color:var(--faint);white-space:nowrap' }, ago(q.created)))) : el('div', { class: 'empty' }, 'Không có yêu cầu nào')),
    el('div', { class: 'ovpills' }, rcnt('all', 'Tất cả', 'all'), rcnt('new', 'Mới', 'new'), rcnt('working', 'Đang xử lý', 'work'), rcnt('done', 'Xong', 'done')));
  const unread = ch.convs.filter(c => c.unread);
  const chatCard = noChat ? null : card('Tin nhắn chưa đọc', null, el('div', { class: 'scrollbox short' }, unread.length ? unread.map(c => el('div', { style: rowCss, onclick: () => { chatSel = c.id; go('chat'); } },
    el('div', { style: 'flex:1;min-width:0' }, el('b', {}, c.name), el('div', { class: 'chat-last' }, c.last)), el('span', { class: 'count badge-red' }, String(c.unread)))) : el('div', { class: 'empty' }, 'Không có tin nhắn mới')),
    el('button', { class: 'btn', style: 'padding:3px 10px;font-size:12px', onclick: () => go('chat') }, 'Mở chat'));
  const gr = Object.entries(o.groups || {}).sort((a, b) => a[0].localeCompare(b[0]));
  const groupsCard = card('Máy online theo nhóm', null, el('div', { class: 'scrollbox short', style: 'display:flex;flex-direction:column;gap:10px' }, gr.length ? gr.map(([nm, s]) => el('div', { style: 'display:flex;align-items:center;gap:10px;cursor:pointer', onclick: () => { MF.group = nm === 'Chưa phân nhóm' ? '__none' : nm; go('machines'); } },
    el('div', { style: 'width:130px;font-size:13px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis' }, nm), el('div', { class: 'bar', style: 'max-width:none' }, el('i', { style: 'width:' + (s.Total ? s.Online / s.Total * 100 : 0) + '%;background:' + (s.Online === s.Total ? 'var(--good)' : s.Online / s.Total >= 0.6 ? 'var(--warn)' : 'var(--crit)') })), el('span', { class: 'mono', style: 'font-size:12.5px;min-width:56px;text-align:right' }, s.Online + '/' + s.Total))) : el('div', { class: 'empty' }, 'Chưa có dữ liệu')));
  // một trang: không cuộn cả trang, mỗi thẻ tự cuộn bên trong
  v.append(el('div', { class: 'ov' }, kpiRow, emptyNote, el('div', { class: 'ovmain' }, health, el('div', { class: 'ovside' }, reqCard, chatCard))));
};

/* ---------- Máy ---------- */
const MF = { q: '', group: 'all', status: 'all', lim: 100, sel: new Set() };
const cmdLabels = { lock: 'Khóa màn hình', restart: 'Khởi động lại', shutdown: 'Tắt máy', wol: 'Bật máy (WoL)', message: 'Gửi tin nhắn', clean: 'Dọn file rác', freeram: 'Giải phóng RAM' };
function sendCmd(ids, type, args, label) {
  return api('POST', ids.length === 1 ? '/api/machines/' + ids[0] + '/command' : '/api/machines/bulk', ids.length === 1 ? { type, args } : { ids, type, args }).then(r => {
    let m = t('{0}: đã gửi cho {1} máy', t(cmdLabels[type] || type), r.sent.length);
    if (r.offline && r.offline.length) m += ' · ' + t('{0} máy offline bị bỏ qua', r.offline.length);
    if (r.denied && r.denied.length) m += ' · ' + t('{0} máy ngoài phạm vi của bạn', r.denied.length);
    toast(m, (r.offline && r.offline.length) || (r.denied && r.denied.length) ? 'warn' : '');
  }).catch(fail);
}
// fixCmd: tự sửa chữa trên một máy (dọn file rác / giải phóng RAM), chờ máy trả kết quả rồi báo.
async function fixCmd(m, type) {
  toast(t('Đang gửi lệnh tới {0}…', m.name));
  try { const d = await runInfoCmd(m.id, type); toast(trErr(d.result || '') || t('Xong'), d.status === 'failed' ? 'warn' : ''); } catch (e) { fail(e); }
}
// runInfoCmd: gửi lệnh procs/smart tới một máy và chờ kết quả (tối đa ~15 giây).
async function runInfoCmd(mid, type) {
  const r = await api('POST', '/api/machines/' + mid + '/command', { type, args: {} });
  const cid = r.cmds && r.cmds[mid];
  if (!cid) throw new Error((r.offline && r.offline.length) ? 'Máy offline' : 'Không gửi được lệnh');
  for (let i = 0; i < 30; i++) {
    await new Promise(res => setTimeout(res, 500));
    const d = await api('GET', '/api/machines/' + mid + '/cmd/' + cid);
    if (d.status === 'done' || d.status === 'failed') return d;
  }
  throw new Error('Máy chưa trả lời, hãy thử lại');
}
function msgModal(ids) {
  const ta = el('textarea', { class: 'input', rows: 3, maxlength: 300, placeholder: 'Nội dung hiện trên màn hình máy (tối đa 300 ký tự)', style: 'width:100%' });
  const f = field('Tin nhắn', ta);
  modal(t('Gửi tin nhắn tới {0} máy', ids.length), f, [{ label: 'Hủy', on: c => c() }, { label: 'Gửi', cls: 'primary', on: c => { if (!setErr(f, ta.value.trim() ? '' : 'Nhập nội dung')) return; c(); sendCmd(ids, 'message', { text: ta.value }); } }]);
}
function groupModal(ids, after) {
  const s = select([['', 'Chưa phân nhóm'], ...groups.map(g => [g, g])], '');
  modal(t('Chuyển {0} máy sang nhóm', ids.length), field('Nhóm', s), [{ label: 'Hủy', on: c => c() }, { label: 'Chuyển', cls: 'primary', on: async c => { try { await api('POST', '/api/machines/bulk', { ids, type: 'group', group: s.value }); c(); toast('Đã chuyển nhóm'); MF.sel.clear(); after ? after() : render(); } catch (e) { fail(e); } } }]);
}
function dangerCmd(ids, type, name) {
  confirmBox(t(cmdLabels[type]) + '?', t(type === 'shutdown' ? 'Tắt {0}. Người đang dùng máy sẽ bị ngắt sau 10 giây và việc này được ghi vào nhật ký.' : 'Khởi động lại {0}. Người đang dùng máy sẽ bị ngắt sau 10 giây và việc này được ghi vào nhật ký.', name), t(cmdLabels[type]), true, () => sendCmd(ids, type));
}
const osBadge = s => (s || '').replace(/^Windows ·\s*/, '').replace(/Microsoft /, '').slice(0, 28) || '—';
VIEWS.machines = async (v, quiet) => {
  const [list, gl] = await Promise.all([api('GET', '/api/machines'), api('GET', '/api/groups')]);
  groups = gl.groups.map(x => x.name);
  const q = MF.q.trim().toLowerCase();
  const rows = list.filter(m => (MF.group === 'all' || (MF.group === '__none' ? !m.group : m.group === MF.group)) && (MF.status === 'all' || (MF.status === 'on') === m.online) && (!q || (m.name + ' ' + m.ip + ' ' + m.mac + ' ' + (m.user || '')).toLowerCase().includes(q)));
  const search = el('input', { class: 'input grow', id: 'msearch', type: 'search', placeholder: 'Tìm theo tên máy, IP, MAC, người dùng…', value: MF.q, oninput: e => { MF.q = e.target.value; MF.lim = 100; render(true); } });
  v.append(el('div', { class: 'filters' }, search,
    select([['all', 'Tất cả nhóm'], ['__none', 'Chưa phân nhóm'], ...groups.map(g => [g, g])], MF.group, x => { MF.group = x; MF.lim = 100; render(); }),
    select([['all', 'Mọi trạng thái'], ['on', 'Đang online'], ['off', 'Offline']], MF.status, x => { MF.status = x; render(); }),
    el('span', { style: 'color:var(--muted);font-size:12.5px' }, t('{0} / {1} máy', rows.length, list.length))));
  const sel = [...MF.sel].filter(id => list.some(m => m.id === id));
  MF.sel = new Set(sel);
  if (sel.length && canWrite()) {
    const b = (l, ty, fn) => el('button', { class: 'btn', onclick: fn || (() => sendCmd(sel, ty)) }, l);
    v.append(el('div', { class: 'card', style: 'margin-top:12px;border-color:var(--accent)' }, el('div', { class: 'card-b', style: 'display:flex;flex-wrap:wrap;gap:9px;align-items:center' },
      el('b', {}, t('Đã chọn {0} máy', sel.length)), b('Khóa màn hình', 'lock'), b('Gửi tin nhắn', 'message', () => msgModal(sel)), b('Bật máy (WoL)', 'wol'), b('Dọn file rác', 'clean'), b('Giải phóng RAM', 'freeram'),
      b('Khởi động lại', 'restart', () => dangerCmd(sel, 'restart', t('{0} máy', sel.length))), b('Tắt máy', 'shutdown', () => dangerCmd(sel, 'shutdown', t('{0} máy', sel.length))),
      b('Chuyển nhóm', null, () => groupModal(sel)), el('button', { class: 'btn', onclick: () => { MF.sel.clear(); render(); } }, 'Bỏ chọn'))));
  }
  const shown = rows.slice(0, MF.lim);
  const allOn = shown.length && shown.every(m => MF.sel.has(m.id));
  const head = [{ t: canWrite() ? el('input', { type: 'checkbox', class: 'sel', checked: allOn, onclick: e => { e.stopPropagation(); shown.forEach(m => e.target.checked ? MF.sel.add(m.id) : MF.sel.delete(m.id)); render(true); } }) : '' }, 'Máy', { t: 'Nhóm', cls: 'hide-s' }, { t: 'IP', cls: 'hide-s' }, 'Trạng thái', 'CPU', { t: 'Nhiệt độ', cls: 'hide-s' }, { t: 'Ổ trống', cls: 'hide-s' }, canWrite() ? 'Điều khiển' : ''];
  const qb = (l, tip, fn, red) => el('button', { class: 'btn', title: tip, style: 'padding:3px 9px;font-size:12px;margin-right:5px' + (red ? ';color:var(--crit)' : ''), onclick: fn }, l);
  const trs = shown.map(m => el('tr', { onclick: () => go('detail', m.id) },
    el('td', { onclick: e => e.stopPropagation() }, canWrite() ? el('input', { type: 'checkbox', class: 'sel', checked: MF.sel.has(m.id), onclick: e => { e.target.checked ? MF.sel.add(m.id) : MF.sel.delete(m.id); render(true); } }) : ''),
    el('td', {}, el('div', { style: 'font-weight:600' }, m.name), el('div', { style: 'font-size:11.5px;color:var(--faint)' }, osBadge(m.os))),
    el('td', { class: 'hide-s' }, m.group || el('span', { style: 'color:var(--faint)' }, 'Chưa phân nhóm')),
    el('td', { class: 'hide-s mono' }, m.ip || '—'),
    el('td', {}, m.online ? pill('ok', 'Online') : pill('off', 'Offline')),
    el('td', {}, m.online ? bar(m.cpu, 70, 90) : '—'),
    el('td', { class: 'hide-s mono' }, m.online && m.temp != null ? el('span', { style: 'color:' + col(m.temp, 70, settings ? settings.temp : 85) }, num(m.temp) + '°C') : '—'),
    el('td', { class: 'hide-s mono' }, m.diskFree != null ? el('span', { style: 'color:' + (m.diskFree <= (settings ? settings.disk : 10) ? 'var(--crit)' : 'inherit') }, num(m.diskFree) + '%') : '—'),
    canWrite() ? el('td', { style: 'white-space:nowrap', onclick: e => e.stopPropagation() }, m.canControl ? (m.online ? [
      qb('Khóa', 'Khóa màn hình', () => sendCmd([m.id], 'lock')), qb('Nhắn', 'Gửi tin nhắn', () => msgModal([m.id])),
      qb('Khởi động lại', 'Khởi động lại', () => dangerCmd([m.id], 'restart', m.name)), qb('Tắt', 'Tắt máy', () => dangerCmd([m.id], 'shutdown', m.name), true)] :
      [qb('Bật máy', 'Bật máy (WoL)', () => sendCmd([m.id], 'wol'))]) : el('span', { style: 'color:var(--faint);font-size:12px' }, 'ngoài nhóm')) : null));
  v.append(el('div', { class: 'card', style: 'margin-top:12px' }, el('div', { class: 'card-b', style: 'padding:0 16px 8px' }, table(head, trs, list.length ? 'Không có máy nào khớp bộ lọc' : 'Chưa có máy nào. Cài MAM Agent trên máy client, máy sẽ tự hiện ở đây.'))));
  if (rows.length > MF.lim) v.append(el('div', { style: 'text-align:center;margin-top:10px' }, el('button', { class: 'btn', onclick: () => { MF.lim += 200; render(true); } }, t('Hiện thêm ({0} máy nữa)', rows.length - MF.lim))));
};

/* ---------- Chi tiết máy ---------- */
function spark(vals, color, max) {
  const pts = vals.filter(x => x != null && x >= 0);
  if (pts.length < 2) return el('div', { style: 'height:46px;color:var(--faint);font-size:12px;padding-top:14px' }, 'Đang thu thập số liệu…');
  const mx = max || Math.max(...pts, 1), w = 300, h = 46, n = vals.length;
  const d = vals.map((x, i) => x == null || x < 0 ? null : (i / (n - 1) * w).toFixed(1) + ',' + (h - 3 - x / mx * (h - 6)).toFixed(1)).filter(Boolean).join(' ');
  return el('div', { html: '<svg class="spark" viewBox="0 0 ' + w + ' ' + h + '" preserveAspectRatio="none"><polyline fill="none" stroke="' + color + '" stroke-width="1.8" stroke-linejoin="round" points="' + d + '"/></svg>' });
}
function liveCard(name, big, unit, color, hist) { return el('div', { class: 'livecard' }, el('div', { class: 'top' }, el('span', { class: 'nm' }, name), el('span', { class: 'big' }, big, el('small', { style: 'font-size:13px;color:var(--faint);margin-left:2px' }, unit))), spark(hist, color, unit === '°C' ? 110 : 100)); }
const srow = (k, val) => el('div', { class: 'srow' }, el('span', {}, k), el('b', { style: 'font-weight:600;text-align:right;max-width:62%;word-break:break-word' }, val == null || val === '' ? '—' : String(val)));

VIEWS.detail = async (v, quiet) => {
  let m;
  try { m = await api('GET', '/api/machines/' + route.id); } catch (e) { go('machines'); throw e; }
  $('#pgTitle').textContent = m.name; $('#pgSub').textContent = t(m.group || 'Chưa phân nhóm') + ' · ' + (m.ip || '—');
  const hw = m.hw || {}, ctl = m.canControl && canWrite();
  const off = !m.online;
  const btn = (l, fn, cls) => el('button', { class: 'btn ' + (cls || ''), onclick: fn }, l);
  const bar1 = el('div', { class: 'toolbar', style: 'margin-top:12px' },
    ctl && !off ? [btn('Khóa màn hình', () => sendCmd([m.id], 'lock')), btn('Gửi tin nhắn', () => msgModal([m.id])), btn('Khởi động lại', () => dangerCmd([m.id], 'restart', m.name)), btn('Tắt máy', () => dangerCmd([m.id], 'shutdown', m.name)), btn('Dọn file rác', () => fixCmd(m, 'clean')), btn('Giải phóng RAM', () => fixCmd(m, 'freeram'))] : null,
    ctl && off ? btn('Bật máy (WoL)', () => sendCmd([m.id], 'wol')) : null,
    ctl ? [btn('Đổi nhóm', () => groupModal([m.id], () => render())), btn('Đổi tên', () => renameModal(m))] : null,
    isSuper() ? btn('Thu hồi token', () => confirmBox('Thu hồi máy này?', t('Máy {0} sẽ bị gỡ khỏi hệ thống và agent không báo cáo được nữa. Cài lại agent mới đăng ký lại được.', m.name), 'Thu hồi', true, async () => { try { await api('DELETE', '/api/machines/' + m.id); toast(t('Đã thu hồi {0}', m.name)); go('machines'); } catch (e) { fail(e); } }), 'danger') : null);
  v.append(el('button', { class: 'back', onclick: () => go('machines') }, '← Danh sách máy'),
    el('div', { class: 'dhead', style: 'margin-top:6px' }, el('div', { class: 'title' }, el('h2', { style: 'font-size:20px' }, m.name), m.online ? pill('ok', 'Online') : pill('off', 'Offline')),
      el('span', { style: 'color:var(--muted);font-size:13px' }, t('Người dùng: {0} · chạy được {1} · agent {2}', m.user || '—', uptime(m.uptime), m.ver || '—') + (off ? ' · ' + t('báo cáo cuối {0}', ago(m.lastSeen)) : ''))),
    bar1);
  if (!m.canControl && me.role === 'tech') v.append(el('div', { style: 'margin-top:12px' }, infoNote('Máy này ngoài nhóm được giao cho bạn nên chỉ xem được, không ra lệnh.')));
  v.append(el('div', { class: 'card', style: 'margin-top:12px' }, el('div', { class: 'card-h' }, el('h3', {}, 'Remote desktop'), el('span', { class: 'sub' }, 'ID TeamViewer/AnyDesk hoặc địa chỉ RDP, tự ghi')),
    el('div', { class: 'card-b', style: 'display:flex;gap:8px;align-items:center;flex-wrap:wrap' },
      m.remoteNote ? el('span', { class: 'mono', style: 'background:var(--surface-2);padding:6px 10px;border-radius:7px' }, m.remoteNote) : el('span', { style: 'color:var(--faint)' }, 'Chưa có — cài TeamViewer/AnyDesk hoặc bật RDP trên máy này rồi ghi ID/địa chỉ vào đây để lần sau bấm mở nhanh'),
      m.remoteNote ? btn('Sao chép', () => { try { navigator.clipboard.writeText(m.remoteNote); toast('Đã sao chép'); } catch (e) { toast('Hãy chọn và chép thủ công', 'warn'); } }) : null,
      ctl ? btn(m.remoteNote ? 'Sửa' : '+ Thêm', () => remoteNoteModal(m)) : null)));
  v.append(el('div', { class: 'card', style: 'margin-top:12px' }, el('div', { class: 'card-h' }, el('h3', {}, 'Xem màn hình'), el('span', { class: 'sub' }, 'phải được người ngồi máy đồng ý, hoặc nhập mật khẩu điều khiển từ xa')),
    el('div', { class: 'card-b', style: 'display:flex;gap:10px;align-items:center;flex-wrap:wrap' },
      el('span', { style: 'color:var(--faint);font-size:12.5px' }, 'Máy sẽ hiện hộp thoại xin phép, hoặc vào thẳng nếu bạn nhập đúng mật khẩu điều khiển từ xa. Người ngồi máy luôn bấm Dừng ngay được bất cứ lúc nào.'),
      ctl && !off ? btn('Xin xem màn hình', () => openRemote(m)) : el('span', { style: 'color:var(--faint)' }, off ? 'Máy offline' : 'Ngoài phạm vi của bạn'))));
  const tabs = el('div', { class: 'tabs', style: 'margin-top:14px' }, [['live', 'Giám sát'], ['hw', 'Phần cứng'], ['disk', 'Ổ cứng'], ['apps', 'Phần mềm'], ['procs', 'Tiến trình'], ['cmd', 'Lệnh gần đây'], ['up', 'Thời gian hoạt động'], ['err', 'Lỗi của máy']].map(([k, l]) => el('button', { class: 'tab' + (route.tab === k ? ' on' : ''), onclick: () => { route.tab = k; render(); } }, l)));
  v.append(tabs);
  const body = el('div', { style: 'margin-top:14px' }); v.append(body);
  const H = m.hist || [];
  if (route.tab === 'live') {
    if (off) body.append(infoNote('Máy đang offline: số liệu bên dưới dừng ở lần báo cáo cuối.'));
    body.append(el('div', { class: 'live', style: off ? 'margin-top:12px;opacity:.6' : 'margin-top:12px' },
      liveCard('CPU', num(m.cpu), '%', 'var(--accent)', H.map(s => s.cpu)),
      liveCard('RAM', num(m.ram), '%', 'var(--good)', H.map(s => s.ram)),
      liveCard('Nhiệt độ CPU', m.temp != null ? num(m.temp) : 'N/A', m.temp != null ? '°C' : '', 'var(--crit)', H.map(s => s.temp)),
      el('div', { class: 'livecard' }, el('div', { class: 'top' }, el('span', { class: 'nm' }, 'Ổ đĩa')), (m.disks || []).map(d => { const used = d.totalGB ? 100 - d.freeGB / d.totalGB * 100 : 0; return el('div', { style: 'margin-top:9px' }, el('div', { style: 'display:flex;justify-content:space-between;font-size:12.5px' }, el('b', {}, d.name), el('span', { class: 'mono' }, t('{0} / {1} GB trống', num(d.freeGB, 0), num(d.totalGB, 0)))), el('div', { class: 'bar', style: 'max-width:none;margin-top:4px' }, el('i', { style: 'width:' + used + '%;background:' + col(used, 80, 100 - (settings ? settings.disk : 10)) }))); }))));
    if (m.temp == null) body.append(el('div', { style: 'margin-top:12px' }, infoNote('Không đọc được nhiệt độ CPU qua Windows (nhiều máy không công khai cảm biến này). Các mục khác vẫn hoạt động bình thường.')));
    body.append(el('div', { style: 'margin-top:12px;font-size:12px;color:var(--faint)' }, 'Biểu đồ lấy mẫu mỗi 30 giây, giữ tối đa 4 giờ gần nhất kể từ lúc server chạy.'));
  } else if (route.tab === 'up') {
    body.append(await upDetail(m.id));
  } else if (route.tab === 'err') {
    body.append(await errTab(m));
  } else if (route.tab === 'hw') {
    const rows = [['Tên máy', m.name], ['Hệ điều hành', hw.os || m.os], ['Phiên bản', hw.osBuild], ['Kiến trúc', hw.arch], ['Hãng và model', hw.model], ['CPU', hw.cpu], ['Nhân / luồng', hw.cores ? hw.cores + ' / ' + (hw.threads || '—') : null], ['RAM', hw.ramGB ? hw.ramGB + ' GB' : null], ['Card đồ họa', hw.gpu], ['Bo mạch chủ', hw.board], ['BIOS', hw.bios], ['Số serial', hw.serial], ['Địa chỉ IP', (hw.ips || [m.ip]).join(', ')], ['Địa chỉ MAC', (hw.macs || [m.mac]).join(', ')], ['Miền / nhóm làm việc', hw.domain], ['Đăng ký lúc', fmtDT(m.registered)]];
    body.append(card('Thông tin máy', 'agent tự đọc, cập nhật mỗi giờ', el('div', { class: 'cols' }, [rows.slice(0, 8), rows.slice(8)].map(r => el('div', {}, r.map(x => srow(x[0], x[1])))))));
  } else if (route.tab === 'disk') {
    const ds = hw.disks || [];
    if (hw.smartFail) body.append(el('div', { style: 'margin-bottom:12px' }, infoNote('Windows báo có ổ cứng dự báo sắp hỏng (S.M.A.R.T). Hãy sao lưu dữ liệu và thay ổ sớm.')));
    body.append(card('Ổ cứng vật lý', 'tình trạng theo Windows', table(['Ổ', 'Loại', 'Dung lượng', 'Tình trạng', 'Nhiệt độ', 'Mòn', 'Giờ chạy'], ds.map(d => el('tr', { class: 'nohover' }, el('td', {}, el('div', { style: 'font-weight:600' }, d.model), el('div', { style: 'font-size:11.5px;color:var(--faint)' }, d.serial || '')), el('td', {}, d.type || '—'), el('td', { class: 'mono' }, d.sizeGB ? d.sizeGB + ' GB' : '—'),
      el('td', {}, /^(ok|healthy)$/i.test(d.health || '') ? pill('ok', d.health) : pill('warn', d.health || 'Không rõ')), el('td', { class: 'mono' }, d.tempC != null ? d.tempC + '°C' : '—'), el('td', { class: 'mono' }, d.wear != null ? d.wear + '%' : '—'), el('td', { class: 'mono' }, d.hours != null ? t('{0} giờ', d.hours) : '—'))), 'Agent chưa gửi thông tin ổ cứng')));
    if (ctl && !off) {
      const smartBox = el('div', { style: 'margin-top:12px' });
      body.append(el('div', { style: 'margin-top:12px' }, el('button', { class: 'btn', onclick: async () => {
        smartBox.innerHTML = ''; smartBox.append(el('div', { style: 'color:var(--muted);font-size:12.5px' }, 'Đang đọc S.M.A.R.T, có thể mất vài giây…'));
        try {
          const d = await runInfoCmd(m.id, 'smart');
          if (d.status === 'failed') { smartBox.innerHTML = ''; smartBox.append(infoNote(trErr(d.result || 'Lỗi'))); return; }
          const rows = d.hasData ? await api('GET', '/api/machines/' + m.id + '/cmd/' + d.id + '/data') : [];
          smartBox.innerHTML = '';
          smartBox.append(card('Kết quả S.M.A.R.T (trực tiếp)', 'vừa đọc từ ' + m.name, table(['Ổ', 'Loại', 'Tình trạng', 'Nhiệt độ', 'Mòn', 'Giờ chạy', 'Lỗi đọc/ghi'], rows.map(x => el('tr', { class: 'nohover' }, el('td', {}, x.name), el('td', {}, x.type || x.bus || '—'),
            el('td', {}, /^(ok|healthy)$/i.test(x.health || '') ? pill('ok', x.health) : pill('warn', x.health || x.status || 'Không rõ')), el('td', { class: 'mono' }, x.temp != null ? x.temp + '°C' : '—'), el('td', { class: 'mono' }, x.wear != null ? x.wear + '%' : '—'), el('td', { class: 'mono' }, x.hours != null ? x.hours : '—'), el('td', { class: 'mono' }, (x.readErr || 0) + ' / ' + (x.writeErr || 0)))), 'Không đọc được')));
        } catch (e) { smartBox.innerHTML = ''; smartBox.append(infoNote(e.message)); }
      } }, 'Kiểm tra S.M.A.R.T ngay'), smartBox));
    }
  } else if (route.tab === 'apps') {
    const apps = hw.apps || [];
    const box = el('div', {}), q = el('input', { class: 'input', placeholder: 'Tìm phần mềm…', style: 'max-width:280px' });
    const draw = () => { const k = q.value.trim().toLowerCase(); const l = apps.filter(a => !k || (a.n + ' ' + (a.p || '')).toLowerCase().includes(k)); box.innerHTML = ''; box.append(table(['Phần mềm', 'Phiên bản', 'Nhà phát hành'], l.map(a => el('tr', { class: 'nohover' }, el('td', { style: 'font-weight:600' }, a.n), el('td', { class: 'mono' }, a.v || '—'), el('td', {}, a.p || '—'))), apps.length ? 'Không có kết quả' : 'Chưa có dữ liệu (agent báo mỗi giờ, chỉ Windows)')); };
    q.oninput = draw; draw();
    body.append(card('Phần mềm đã cài', t('{0} mục, agent tự đọc mỗi giờ', apps.length), el('div', {}, el('div', { style: 'margin-bottom:10px' }, q), box)));
    if (hw.rebootPending) body.append(el('div', { style: 'margin-top:12px' }, infoNote('Windows đang chờ khởi động lại để hoàn tất cập nhật.')));
  } else if (route.tab === 'procs') {
    if (off) { body.append(infoNote('Máy đang offline, không xem được tiến trình đang chạy.')); }
    else {
      const list = el('div', {});
      function killBtn(x) {
        if (!ctl) return null;
        const go = () => confirmBox('Tắt tiến trình?', t('Tắt {0} (PID {1}) trên {2}.', x.name, x.pid, m.name), 'Tắt', true, async () => {
          try { await api('POST', '/api/machines/' + m.id + '/command', { type: 'kill', args: { pid: String(x.pid), name: x.name } }); toast('Đã gửi lệnh tắt tiến trình'); setTimeout(load, 1500); }
          catch (e) { fail(e); }
        });
        return el('td', { style: 'text-align:right' }, el('button', { class: 'btn danger', style: 'padding:3px 9px;font-size:12px', onclick: go }, 'Tắt'));
      }
      function procRow(x) {
        return el('tr', { class: 'nohover' }, el('td', { class: 'mono' }, x.pid), el('td', { style: 'font-weight:600' }, x.name), el('td', { class: 'mono' }, x.mem), el('td', { class: 'mono' }, x.cpu), el('td', {}, x.title || '—'), killBtn(x));
      }
      const load = async () => {
        list.innerHTML = '';
        list.append(el('div', { style: 'color:var(--muted);font-size:12.5px' }, 'Đang lấy danh sách tiến trình…'));
        try {
          const d = await runInfoCmd(m.id, 'procs');
          if (d.status === 'failed') { list.innerHTML = ''; list.append(infoNote(trErr(d.result || 'Lỗi'))); return; }
          const rows = d.hasData ? await api('GET', '/api/machines/' + m.id + '/cmd/' + d.id + '/data') : [];
          rows.sort((a, b) => b.mem - a.mem);
          const heads = ['PID', 'Tên', 'RAM (MB)', 'CPU (giây)', 'Tiêu đề cửa sổ', ctl ? '' : null];
          list.innerHTML = '';
          list.append(card('Tiến trình (theo RAM, tối đa 80)', m.name, table(heads, rows.map(procRow), 'Không có dữ liệu')));
        } catch (e) { list.innerHTML = ''; list.append(infoNote(e.message)); }
      };
      body.append(el('div', {}, el('button', { class: 'btn primary', onclick: load }, 'Xem tiến trình đang chạy'), el('div', { style: 'margin-top:12px' }, list)));
    }
  } else {
    body.append(card('Lệnh gần đây', 'tối đa 15 lệnh', table(['Lúc', 'Lệnh', 'Người gửi', 'Kết quả'], (m.commands || []).map(c => el('tr', { class: 'nohover' }, el('td', {}, fmtDT(c.created)), el('td', {}, t(cmdLabels[c.type] || c.type) + (c.args && c.args.text ? ': ' + c.args.text : '')), el('td', {}, c.by), el('td', {}, c.status === 'done' ? el('span', { class: 'cmdok' }, 'Đã thực hiện') : c.status === 'failed' ? el('span', { class: 'cmdbad' }, t('Lỗi: {0}', trErr(c.result || ''))) : el('span', { style: 'color:var(--muted)' }, c.status === 'sent' ? 'Đang thực hiện…' : 'Chờ máy nhận')))), 'Chưa có lệnh nào')));
  }
};
function remoteNoteModal(m) {
  const i = input('Ví dụ: TeamViewer 123 456 789, hoặc rdp://192.168.1.20', 'text', m.remoteNote || ''), f = field('ID TeamViewer/AnyDesk hoặc địa chỉ RDP', i);
  modal('Remote desktop', [infoNote('MAM chỉ ghi nhớ thông tin này, không tự kết nối hay điều khiển máy. Mở bằng phần mềm remote desktop đã cài trên máy đại ca.'), f],
    [{ label: 'Hủy', on: c => c() }, { label: 'Lưu', cls: 'primary', on: async c => { try { await api('PUT', '/api/machines/' + m.id + '/remote-note', { note: i.value }); c(); render(); } catch (e) { setErr(f, e.message); } } }]);
}
// screenViewModal: xin xem màn hình (phải được người ngồi máy đồng ý), rồi hiện khung hình liên tục
// và chuyển chuột/phím của admin thành thao tác trên máy đó.
// openRemote: mở cửa sổ riêng để điều khiển; trình duyệt chặn cửa sổ bật lên thì dùng khung phủ kín trang này.
function openRemote(m) {
  // Cửa sổ riêng (có nút thu nhỏ / phóng to / đóng của trình duyệt), cỡ 80% màn hình và căn giữa.
  const sw = screen.availWidth || 1366, sh = screen.availHeight || 800, w = Math.round(sw * .8), h = Math.round(sh * .8);
  let win = null;
  try { win = window.open('/?remote=' + encodeURIComponent(m.id) + '&n=' + encodeURIComponent(m.name), 'mamremote-' + m.id, 'popup=yes,width=' + w + ',height=' + h + ',left=' + Math.round((sw - w) / 2) + ',top=' + Math.round((sh - h) / 2) + ',resizable=yes'); } catch (e) { }
  if (!win) { toast('Trình duyệt chặn cửa sổ bật lên. Cho phép cửa sổ bật lên với địa chỉ này để remote mở thành cửa sổ riêng.', 'err'); screenViewModal(m); } else try { win.focus(); } catch (e) { }
}
function screenViewModal(m, standalone) {
  let sid = null, closed = false, active = false, seq = 0, lastMove = 0, viewOnly = false, lastChatLen = -1, frames = 0, prevURL = null, fitMode = 'fit';
  const held = new Set(), tog = {};
  const base = '/api/machines/' + m.id + '/screen/';
  let mini = false;
  const send = ev => { if (sid && active && !viewOnly && !mini) api('POST', base + sid + '/input', ev).catch(() => { }); };
  const img = el('canvas', { class: 'vnc-img', width: '16', height: '9' }), ictx = img.getContext('2d');
  const stage = el('div', { class: 'vnc-stage', tabindex: '0' }, img);
  const statusEl = el('span', { class: 'vnc-status' }, 'Đang gửi yêu cầu…');
  const fpsEl = el('span', { class: 'vnc-fps' }, '');
  const msgEl = el('div', { class: 'vnc-wait' }, 'Đang gửi yêu cầu…');
  stage.append(msgEl);
  // --- chuột ---
  const relXY = e => { const r = img.getBoundingClientRect(); return { x: Math.min(1, Math.max(0, (e.clientX - r.left) / r.width)), y: Math.min(1, Math.max(0, (e.clientY - r.top) / r.height)) }; };
  const btnName = b => b === 2 ? 'right' : b === 1 ? 'middle' : 'left';
  img.addEventListener('mousemove', e => { const n = Date.now(); if (n - lastMove < 40) return; lastMove = n; send(Object.assign({ type: 'move' }, relXY(e))); });
  let fsAuto = false, hookOk = false; // hookOk: Console desktop đang giữ phím Win giúp, khỏi cần toàn màn hình
  // Lần bấm đầu vào màn hình điều khiển: tự vào toàn màn hình để bật khóa phím (không thì phím Win/Alt+Tab thật vẫn mở Start/Run trên máy của bạn). Chỉ tự làm một lần mỗi phiên.
  img.addEventListener('mousedown', e => { if (!fsAuto && !hookOk && !viewOnly && !mini && !document.fullscreenElement) { fsAuto = true; try { root.requestFullscreen().catch(() => { }); } catch (x) { } } });
  img.addEventListener('mousedown', e => { e.preventDefault(); stage.focus(); send(Object.assign({ type: 'down', button: btnName(e.button) }, relXY(e))); });
  window.addEventListener('mouseup', e => { if (closed) return; if (e.target === img) { e.preventDefault(); send({ type: 'up', button: btnName(e.button) }); } });
  img.addEventListener('contextmenu', e => e.preventDefault());
  img.addEventListener('wheel', e => { e.preventDefault(); send({ type: 'wheel', delta: -e.deltaY }); }, { passive: false });
  // --- bàn phím: gửi phím vật lý lên/xuống như VNC, nên Ctrl+C, Alt+Tab, phím mũi tên... đều chạy ---
  stage.addEventListener('keydown', e => { if (viewOnly || e.target !== stage) return; // ô nhập mật khẩu nằm trong khung: không chặn phím của nó
    e.preventDefault(); e.stopPropagation(); if (e.repeat && /^(Shift|Control|Alt|Meta)/.test(e.code)) return; held.add(e.code); send({ type: 'kdown', code: e.code }); });
  stage.addEventListener('keyup', e => { if (viewOnly || e.target !== stage) return; e.preventDefault(); held.delete(e.code); send({ type: 'kup', code: e.code }); });
  stage.addEventListener('blur', () => { held.forEach(c => send({ type: 'kup', code: c })); held.clear(); });
  const keyTap = codes => { codes.forEach(c => send({ type: 'kdown', code: c })); [...codes].reverse().forEach(c => send({ type: 'kup', code: c })); stage.focus(); };
  // --- thanh công cụ ---
  const tbtn = (label, on, cls, title) => el('button', { class: 'vnc-b ' + (cls || ''), title: title ? t(title) : '', onclick: on }, label);
  const sticky = (label, code) => { const b = tbtn(label, () => { tog[code] = !tog[code]; b.classList.toggle('on', tog[code]); send({ type: tog[code] ? 'kdown' : 'kup', code }); stage.focus(); }, '', t('Giữ phím {0} cho các thao tác tiếp theo', label)); return b; };
  const fitSel = el('select', { class: 'vnc-sel', onchange: e => { fitMode = e.target.value; stage.dataset.fit = fitMode; } },
    ...[['fit', 'Vừa khung'], ['one', 'Kích thước thật 1:1'], ['stretch', 'Kéo dãn']].map(([v, l]) => el('option', { value: v }, t(l))));
  stage.dataset.fit = 'fit';
  const voBtn = tbtn(t('Chỉ xem'), () => { viewOnly = !viewOnly; voBtn.classList.toggle('on', viewOnly); stage.classList.toggle('vo', viewOnly); }, '', 'Chỉ nhìn, không gửi chuột/phím');
  const textBox = el('textarea', { class: 'input', rows: 3, maxlength: 500, placeholder: t('Gõ nội dung (cả tiếng Việt) rồi bấm Gõ vào máy…') });
  const textPanel = el('div', { class: 'vnc-panel', hidden: true }, el('b', {}, t('Gõ văn bản vào máy')), textBox,
    el('div', { style: 'display:flex;gap:8px;justify-content:flex-end' }, tbtn(t('Gõ vào máy'), () => { const x = textBox.value; if (x && active && !viewOnly) { send({ type: 'text', text: x }); textBox.value = ''; } stage.focus(); }, 'pri')));
  const chatList = el('div', { class: 'vnc-chatlist' });
  const chatIn = el('input', { class: 'input', placeholder: t('Nhắn trong lúc xem màn hình…'), maxlength: '300', style: 'flex:1' });
  const chatSend = async () => { const v = chatIn.value.trim(); if (!v || !sid) return; chatIn.value = ''; try { await api('POST', base + sid + '/msg', { text: v }); } catch (e) { fail(e); } };
  chatIn.addEventListener('keydown', e => { if (e.key === 'Enter') { e.preventDefault(); chatSend(); } });
  const chatPanel = el('div', { class: 'vnc-panel', hidden: true }, el('b', {}, t('Chat với người ngồi máy')), chatList, el('div', { style: 'display:flex;gap:6px' }, chatIn, tbtn(t('Gửi'), chatSend, 'pri')));
  const drawChat = chat => {
    if (!chat || chat.length === lastChatLen) return; lastChatLen = chat.length;
    chatList.innerHTML = '';
    chat.forEach(c => chatList.append(el('div', {}, el('b', {}, (c.mine ? t('Bạn') : m.name) + ': '), c.text)));
    chatList.scrollTop = chatList.scrollHeight;
    if (chat.length && !chat[chat.length - 1].mine) chatPanel.hidden = false;
  };
  const toggle = (panel, other) => () => { panel.hidden = !panel.hidden; if (!panel.hidden) other.hidden = true; };
  const root = el('div', { class: 'vnc' });
  const fsBtn = tbtn(t('Toàn màn hình'), () => { if (document.fullscreenElement) document.exitFullscreen(); else root.requestFullscreen && root.requestFullscreen().catch(() => { }); }, '', 'Toàn màn hình: phím Win, Alt+Tab, Esc... chỉ gửi sang máy bị điều khiển, không tác động máy của bạn (thoát bằng nút này)');
  // Khóa bàn phím (Keyboard Lock, Chrome/Edge, chỉ hoạt động khi toàn màn hình): giữ các phím hệ thống lại cho trang, khỏi mở Run/Start/chuyển cửa sổ trên máy đang điều khiển
  const lockKeys = () => { try { if (document.fullscreenElement && navigator.keyboard && navigator.keyboard.lock) navigator.keyboard.lock().catch(() => { }); } catch (e) { } };
  const unlockKeys = () => { try { navigator.keyboard && navigator.keyboard.unlock && navigator.keyboard.unlock(); } catch (e) { } };
  document.addEventListener('fullscreenchange', () => { if (closed) return; if (document.fullscreenElement) { lockKeys(); stage.focus(); } else unlockKeys(); });
  const disconnect = () => {
    if (closed) return; closed = true;
    held.forEach(c => { if (sid) api('POST', base + sid + '/input', { type: 'kup', code: c }).catch(() => { }); });
    if (sid) api('POST', base + sid + '/stop', {}).catch(() => { });
    unlockKeys(); if (document.fullscreenElement) document.exitFullscreen().catch(() => { });
    if (prevURL) URL.revokeObjectURL(prevURL);
    clearInterval(poll); clearInterval(fpsT); root.remove(); if (pwResolve) pwResolve(null);
    if (standalone) window.close();
  };
  if (standalone) window.addEventListener('pagehide', () => { if (!closed && sid) fetch(base + sid + '/stop', { method: 'POST', keepalive: true, credentials: 'same-origin', headers: { 'X-MAM': '1', 'Content-Type': 'application/json' }, body: '{}' }); });
  const bar = el('div', { class: 'vnc-bar' },
    el('div', { class: 'vnc-title' }, el('b', {}, m.name), statusEl, fpsEl),
    el('div', { class: 'vnc-tools' }, fitSel, voBtn, sticky('Ctrl', 'ControlLeft'), sticky('Alt', 'AltLeft'), sticky('Shift', 'ShiftLeft'), sticky('Win', 'MetaLeft'),
      tbtn('Ctrl+Shift+Esc', () => keyTap(['ControlLeft', 'ShiftLeft', 'Escape']), '', 'Mở Quản lý tác vụ. Windows không cho gửi Ctrl+Alt+Del từ phiên người dùng'),
      tbtn('Win+R', () => keyTap(['MetaLeft', 'KeyR']), '', 'Mở hộp Run trên máy bị điều khiển. Ngoài chế độ toàn màn hình, phím Win thật vẫn tác động máy của bạn, dùng nút này'),
      tbtn('Win', () => keyTap(['MetaLeft']), '', 'Mở menu Start trên máy bị điều khiển'),
      tbtn('Alt+Tab', () => keyTap(['AltLeft', 'Tab'])),
      tbtn(t('Gõ văn bản'), toggle(textPanel, chatPanel)), tbtn('Chat', toggle(chatPanel, textPanel)), fsBtn,
      standalone ? null : tbtn(t('Thu nhỏ'), () => setMini(true), '', 'Thu nhỏ thành cửa sổ nhỏ ở góc, phiên vẫn chạy'),
      tbtn(t('Đóng'), disconnect, 'danger')));
  // thu nhỏ: cửa sổ nổi ở góc phải dưới, vẫn thấy hình, bấm Mở lại để điều khiển tiếp hoặc Đóng để ngắt
  const miniBar = el('div', { class: 'vnc-minibar' }, el('b', {}, m.name), el('span', { class: 'vnc-status' }, ''), el('span', { style: 'flex:1' }),
    tbtn(t('Mở lại'), () => setMini(false)), tbtn(t('Đóng'), disconnect, 'danger'));
  const setMini = on => { mini = on; root.classList.toggle('mini', on); if (!on) stage.focus(); else { held.forEach(c => api('POST', base + sid + '/input', { type: 'kup', code: c }).catch(() => { })); held.clear(); } };
  // thanh công cụ ẩn: rê chuột vào mũi tên ở giữa phía trên thì thanh trượt xuống, rời chuột thì tự cất (nhờ vậy hình máy kia choán trọn màn hình)
  const hot = el('button', { class: 'vnc-hot', title: t('Hiện thanh công cụ'), 'aria-label': t('Hiện thanh công cụ') });
  hot.innerHTML = '<svg width="16" height="10" viewBox="0 0 16 10" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M2 2l6 6 6-6"/></svg>';
  let barT; const showBar = on => { clearTimeout(barT); root.classList.toggle('bar-on', on); };
  hot.onmouseenter = hot.onclick = () => showBar(true);
  bar.onmouseenter = () => clearTimeout(barT);
  bar.onmouseleave = () => { barT = setTimeout(() => { if (!document.activeElement || document.activeElement.tagName !== 'SELECT') showBar(false); }, 400); };
  stage.addEventListener('mousedown', () => showBar(false));
  root.append(miniBar, bar, hot, stage, textPanel, chatPanel);
  // Console desktop trên Windows giữ phím Win (Win+R, Win+D...) lại ở mức hệ điều hành rồi đưa qua cổng cục bộ; ở đây nhận và gửi sang máy bị điều khiển
  (async () => {
    const H = 'http://127.0.0.1:47640';
    try { const r = await fetch(H + '/hello', { cache: 'no-store' }); if (!r.ok) return; } catch (e) { return; }
    hookOk = true;
    let n = -1;
    while (!closed) {
      try {
        const d = await (await fetch(H + '/k?after=' + n, { cache: 'no-store' })).json(); n = d.seq;
        if (document.hasFocus() && !viewOnly && !mini) d.ev.forEach(x => { if (x.t === 'kdown') held.add(x.c); else held.delete(x.c); send({ type: x.t, code: x.c }); });
      } catch (e) { await new Promise(r => setTimeout(r, 1000)); }
    }
  })();
  showBar(true); barT = setTimeout(() => showBar(false), 2500); // mở phiên: hiện thanh vài giây cho người dùng thấy rồi cất
  document.body.append(root); stage.focus();
  // --- vòng lấy hình liên tục (server giữ yêu cầu tới khi có hình mới) ---
  let fpsN = 0;
  const fpsT = setInterval(() => { fpsEl.textContent = active ? fpsN + ' fps' : ''; fpsN = 0; }, 1000);
  const frameLoop = async () => {
    while (!closed) {
      if (!active) { await new Promise(r => setTimeout(r, 200)); continue; }
      try {
        const r = await fetch(base + sid + '/frame?after=' + seq, { cache: 'no-store' });
        if (r.status === 204) continue;
        if (!r.ok) { await new Promise(r2 => setTimeout(r2, 300)); continue; }
        seq = +r.headers.get('X-Seq') || seq + 1;
        // gói: u32 độ dài | u16 W | u16 H | u16 số vùng | mỗi vùng u16 x,y,w,h + u32 độ dài JPEG + JPEG (little endian)
        const buf = await r.arrayBuffer(), dv = new DataView(buf);
        let o = 0;
        while (o + 10 <= buf.byteLength) {
          const end = o + 4 + dv.getUint32(o, true);
          let p = o + 4;
          const W = dv.getUint16(p, true), H = dv.getUint16(p + 2, true), n = dv.getUint16(p + 4, true);
          p += 6;
          if (img.width !== W || img.height !== H) { img.width = W; img.height = H; }
          for (let i = 0; i < n; i++) {
            const x = dv.getUint16(p, true), y = dv.getUint16(p + 2, true), jl = dv.getUint32(p + 8, true);
            const bmp = await createImageBitmap(new Blob([new Uint8Array(buf, p + 12, jl)], { type: 'image/jpeg' }));
            ictx.drawImage(bmp, x, y); if (bmp.close) bmp.close();
            p += 12 + jl;
          }
          o = end; fpsN++;
        }
        msgEl.hidden = true;
      } catch (e) { await new Promise(r2 => setTimeout(r2, 500)); }
    }
  };
  // askRemotePw: hộp nhập mật khẩu ngay trong khung màn hình; trả null nếu đóng cửa sổ.
  let pwForm = null, pwResolve = null, noFrame = 0;
  const askRemotePw = e => new Promise(res => {
    pwResolve = res;
    const i = input('Mật khẩu điều khiển từ xa', 'password'), er = el('div', { class: 'vnc-pwerr' }, e || '');
    const go = () => { if (!i.value) return; const f = pwForm; pwForm = null; f.remove(); msgEl.hidden = false; msgEl.textContent = t('Đang kết nối…'); res(i.value); };
    i.onkeydown = ev => { if (ev.key === 'Enter') go(); };
    pwForm = el('div', { class: 'vnc-pw' }, el('b', {}, 'Nhập mật khẩu điều khiển từ xa'), el('span', {}, m.name), i, er, el('button', { class: 'btn primary', onclick: go }, 'Kết nối'));
    msgEl.hidden = true; stage.append(pwForm); i.focus();
  });
  const poll = setInterval(async () => {
    if (closed || !sid) return;
    try {
      const st = await api('GET', base + sid);
      if (st.status === 'pending') { statusEl.textContent = t('Đang chờ máy đồng ý…'); msgEl.textContent = t('Đang chờ người ngồi máy bấm Đồng ý…'); return; }
      if (st.status === 'active') {
        active = true; statusEl.textContent = (st.w && st.h ? st.w + '×' + st.h + ' · ' : '') + t('Đã kết nối'); drawChat(st.chat);
        // đã được đồng ý nhưng chưa có hình: không còn "chờ bấm Đồng ý" nữa, báo đúng tình trạng
        if (!st.w) { noFrame = noFrame || Date.now(); msgEl.hidden = false; msgEl.textContent = st.agentVer && st.agentVer.split('.').map(Number).reduce((a, x) => a * 1000 + x, 0) < 1001010 ? t('Agent trên máy này là bản {0}, cần 1.1.10 trở lên mới báo được lý do. Cài MAM-Server-Setup.exe bản mới trên máy server để các máy tự cập nhật.', st.agentVer) : st.diag ? t('Máy đã đồng ý nhưng chưa gửi được hình. Lý do: {0}', trErr(st.diag)) : t(Date.now() - noFrame > 6000 ? 'Máy đã đồng ý nhưng chưa gửi được hình. Có thể màn hình đang khóa, cửa sổ Remote Desktop bị thu nhỏ hoặc chưa có ai đăng nhập. Xem agent.log trên máy đó.' : 'Máy đã đồng ý, đang chờ hình đầu tiên…'); }
        return;
      }
      active = false; statusEl.textContent = trErr(st.reason) || t('Phiên đã kết thúc'); msgEl.hidden = false; msgEl.textContent = statusEl.textContent; clearInterval(poll);
    } catch (e) { statusEl.textContent = trErr(e.message); }
  }, 700);
  (async () => {
    let pwSet = false, pw = '', err = '';
    try { pwSet = (await api('GET', '/api/remote-password')).set; } catch (e) { }
    for (;;) {
      if (closed) return;
      if (pwSet) pw = await askRemotePw(err); // có mật khẩu điều khiển từ xa: nhập đúng là vào thẳng, không cần máy đồng ý
      if (pw === null) return;
      try { sid = (await api('POST', '/api/machines/' + m.id + '/screen', pwSet ? { password: pw } : {})).id; break; } catch (e) {
        err = trErr(e.message); statusEl.textContent = err; msgEl.textContent = err;
        if (!pwSet) return;
      }
    }
    msgEl.hidden = false; msgEl.textContent = t(pwSet ? 'Đang kết nối…' : 'Đang chờ người ngồi máy bấm Đồng ý…');
    frameLoop();
  })();
}
function renameModal(m) {
  const i = input('Tên máy', 'text', m.name), f = field('Tên máy', i);
  modal('Đổi tên máy', f, [{ label: 'Hủy', on: c => c() }, { label: 'Lưu', cls: 'primary', on: async c => { if (!setErr(f, i.value.trim() ? '' : 'Nhập tên')) return; try { await api('PUT', '/api/machines/' + m.id + '/rename', { name: i.value }); c(); render(); } catch (e) { setErr(f, e.message); } } }]);
}

/* ---------- Nhóm máy ---------- */
VIEWS.groups = async v => {
  const g = await api('GET', '/api/groups'); groups = g.groups.map(x => x.name);
  const add = input('Tên nhóm mới, ví dụ Tầng 4 · Họp'), f = field('', add);
  if (isSuper()) v.append(el('div', { class: 'filters' }, el('div', { class: 'grow' }, add), el('button', { class: 'btn primary', onclick: async () => { if (!add.value.trim()) { toast(t('Nhập tên nhóm'), 'warn'); add.focus(); return; } try { await api('POST', '/api/groups', { name: add.value }); toast('Đã tạo nhóm'); render(); } catch (e) { fail(e); } } }, '+ Tạo nhóm')));
  else v.append(infoNote('Chỉ Super admin tạo và xóa nhóm.'));
  add.onkeydown = e => { if (e.key === 'Enter') { const b = add.closest('.filters') && add.closest('.filters').querySelector('.btn.primary'); if (b) b.click(); } };
  const rows = [...g.groups.map(x => [x.name, x.count, true]), ['Chưa phân nhóm', g.ungrouped, false]];
  v.append(el('div', { class: 'card', style: 'margin-top:12px' }, el('div', { class: 'card-b', style: 'padding:0 16px 8px' }, table(['Nhóm', 'Số máy', ''], rows.map(([n, c, del]) => el('tr', { onclick: () => { MF.group = del ? n : '__none'; go('machines'); } }, el('td', { style: 'font-weight:600' }, n), el('td', { class: 'mono' }, c), el('td', { style: 'text-align:right' }, del && isSuper() ? el('button', { class: 'btn danger', style: 'padding:4px 10px;font-size:12px', onclick: e => { e.stopPropagation(); confirmBox('Xóa nhóm?', t('Xóa nhóm {0}. {1} máy trong nhóm quay về Chưa phân nhóm, không mất máy.', n, c), 'Xóa', true, async () => { try { await api('DELETE', '/api/groups?name=' + encodeURIComponent(n)); toast('Đã xóa nhóm'); render(); } catch (er) { fail(er); } }); } }, 'Xóa') : '')), 'Chưa có nhóm nào')))));
  if (canWrite()) v.append(el('div', { style: 'margin-top:12px' }, await schedCard(groups)));
};

// Lịch hẹn giờ gửi lệnh cho cả nhóm (chạy theo giờ của server)
async function schedCard(g) {
  const list = (await api('GET', '/api/schedules')) || [];
  const dn = ['CN', 'T2', 'T3', 'T4', 'T5', 'T6', 'T7'];
  const box = el('div', {});
  const draw = (l) => { l = l || []; box.innerHTML = ''; box.append(table(['Tên', 'Lệnh', 'Nhóm', 'Giờ', 'Ngày', 'Lần cuối', ''], l.map(x => el('tr', { class: 'nohover' },
    el('td', { style: 'font-weight:600' }, x.name), el('td', {}, t(cmdLabels[x.type] || x.type)), el('td', {}, x.group === '' ? t('Tất cả máy') : x.group === '__none' ? t('Chưa phân nhóm') : x.group), el('td', { class: 'mono' }, x.time),
    el('td', {}, (x.days || []).length ? x.days.map(d => dn[d]).join(' ') : t('Mỗi ngày')), el('td', {}, x.lastRun ? x.lastRun + (x.result ? ' · ' + x.result : '') : '—'),
    el('td', { style: 'text-align:right;white-space:nowrap' },
      el('button', { class: 'btn', style: 'padding:3px 9px;font-size:12px', onclick: async () => { try { await api('PUT', '/api/schedules?id=' + x.id); render(); } catch (e) { fail(e); } } }, x.enabled ? 'Tắt' : 'Bật'), ' ',
      el('button', { class: 'btn danger', style: 'padding:3px 9px;font-size:12px', onclick: () => confirmBox('Xóa lịch?', t('Xóa lịch {0}.', x.name), 'Xóa', true, async () => { try { await api('DELETE', '/api/schedules?id=' + x.id); render(); } catch (e) { fail(e); } }) }, 'Xóa')))), 'Chưa có lịch nào')); };
  draw(list);
  const addBtn = el('button', { class: 'btn primary', onclick: () => {
    const nm = input('Tên lịch (tùy chọn)'), ty = select(Object.keys(cmdLabels).map(k => [k, t(cmdLabels[k])]), 'shutdown'),
      gr = select([['', t('Tất cả máy')], ['__none', t('Chưa phân nhóm')], ...g.map(x => [x, x])], ''), tm = input('', 'time', '22:00'), msg = input('Nội dung tin nhắn (chỉ cho lệnh Gửi tin nhắn)');
    const ds = dn.map((d, i) => { const c = el('input', { type: 'checkbox', value: i }); return [c, el('label', { style: 'margin-right:10px;font-size:13px' }, c, ' ' + d)]; });
    modal('Thêm lịch hẹn giờ', el('div', {}, field('Tên', nm), field('Lệnh', ty), field('Nhóm máy', gr), field('Giờ chạy (giờ của server)', tm), field('Nội dung', msg), field('Ngày trong tuần', el('div', {}, ds.map(x => x[1])), 'Không chọn ngày nào = chạy mỗi ngày. Máy offline lúc đó sẽ bị bỏ qua (trừ Bật máy qua WoL).')),
      [{ label: 'Hủy', on: c => c() }, { label: 'Lưu', cls: 'primary', on: async c => { try { await api('POST', '/api/schedules', { name: nm.value, type: ty.value, group: gr.value, time: tm.value, days: ds.filter(x => x[0].checked).map(x => +x[0].value), args: { text: msg.value } }); c(); render(); } catch (e) { fail(e); } } }]);
  } }, 'Thêm lịch');
  return card('Lịch hẹn giờ', 'gửi lệnh tự động cho cả nhóm', el('div', {}, el('div', { style: 'margin-bottom:10px' }, addBtn), box));
}

/* ---------- Cảnh báo ---------- */
// Thanh lọc dùng chung: nút chọn (seg) + ô tìm; đổi là vẽ lại ngay, ô tìm giữ con trỏ nhờ id.
const segBar = (opts, cur, on) => el('div', { class: 'seg' }, opts.map(([k, l]) => el('button', { class: cur === k ? 'on' : '', onclick: () => on(k) }, l)));
const searchBox = (id, val, on, ph) => el('input', { id, class: 'input', value: val, placeholder: ph, style: 'max-width:240px', oninput: e => { on(e.target.value); render(true); } });
const hit = (q, ...f) => !q || f.some(x => String(x || '').toLowerCase().includes(q.trim().toLowerCase()));
const AF = { st: 'open', sev: 'all', q: '' };
VIEWS.alerts = async v => {
  let a = await api('GET', '/api/alerts?status=' + AF.st);
  // nghiêm trọng lên đầu, rồi mới nhất; lọc theo mức và tên máy/nội dung
  a = a.filter(x => (AF.sev === 'all' || x.sev === AF.sev) && hit(AF.q, x.name, x.msg)).sort((p, q) => (q.sev === 'crit') - (p.sev === 'crit') || String(q.opened).localeCompare(String(p.opened)));
  v.append(el('div', { class: 'filters' }, el('div', { class: 'seg' }, [['open', 'Đang mở'], ['closed', 'Đã đóng']].map(([k, l]) => el('button', { class: AF.st === k ? 'on' : '', onclick: () => { AF.st = k; render(); } }, l))), el('span', { style: 'color:var(--muted);font-size:12.5px' }, t('{0} cảnh báo', a.length))));
  v.append(el('div', { class: 'filters', style: 'margin-top:10px' }, segBar([['all', 'Mọi mức'], ['crit', 'Nghiêm trọng'], ['warn', 'Cảnh báo']], AF.sev, k => { AF.sev = k; render(); }), searchBox('afq', AF.q, x => AF.q = x, t('Tìm máy hoặc nội dung…'))));
  v.append(el('div', { class: 'card', style: 'margin-top:12px' }, el('div', { class: 'card-b', style: 'padding:0 16px 8px' }, table(['Mức', 'Máy', 'Nội dung', 'Mở lúc', AF.st === 'closed' ? 'Đóng bởi' : '', ''], a.map(x => el('tr', { onclick: () => x.machine === '__server' ? go('settings') : go('detail', x.machine) }, el('td', {}, pill(x.sev === 'crit' ? 'crit' : 'warn', x.sev === 'crit' ? 'Nghiêm trọng' : 'Cảnh báo')), el('td', { style: 'font-weight:600' }, x.name), el('td', {}, x.msg), el('td', {}, fmtDT(x.opened)), el('td', {}, AF.st === 'closed' ? (x.by || '—') + ' · ' + fmtDT(x.closed) : ''),
    el('td', { style: 'text-align:right' }, AF.st === 'open' && canWrite() ? el('button', { class: 'btn', style: 'padding:4px 10px;font-size:12px', onclick: async e => { e.stopPropagation(); try { await api('POST', '/api/alerts/' + x.id + '/close', {}); render(); } catch (er) { fail(er); } } }, 'Đóng') : ''))), AF.st === 'open' ? 'Không có cảnh báo đang mở' : 'Chưa có cảnh báo nào đã đóng'))));
  v.append(el('div', { style: 'margin-top:12px' }, infoNote('Cảnh báo tự mở khi máy vượt ngưỡng (chỉnh ở mục Cài đặt) và tự đóng khi hết vượt ngưỡng. Mất kết nối quá số phút đã đặt cũng tạo cảnh báo.')));
};

/* ---------- Yêu cầu hỗ trợ ---------- */
const RF = { st: 'open', type: 'all', q: '', from: '', to: '' };
const dayOf = s => { const d = new Date(s); return d.getFullYear() + '-' + String(d.getMonth() + 1).padStart(2, '0') + '-' + String(d.getDate()).padStart(2, '0'); };
const reqStatusLabel = { new: 'Mới', working: 'Đang xử lý', done: 'Đã xong' };
function solveModal(q, pre) {
  const ta = el('textarea', { class: 'input', rows: 4, maxlength: 500, placeholder: 'Ví dụ: Cắm lại cáp nguồn, thay nguồn máy 450W; hoặc: Cài lại driver máy quét mã vạch…', style: 'width:100%' }, pre || '');
  modal(t('Đã xử lý xong · {0}', q.name), el('div', {}, el('div', { style: 'font-size:12.5px;margin-bottom:8px;color:var(--muted)' }, 'Ghi lại cách xử lý — lần sau gặp sự cố tương tự hệ thống sẽ gợi ý lại cho cả nhóm kỹ thuật.'), ta),
    [{ label: 'Hủy', on: c => { c(); render(); } }, { label: 'Xác nhận đã xong', cls: 'primary', on: async c => {
      if (!ta.value.trim()) { toast('Hãy nhập cách xử lý'); return; }
      try { await api('PUT', '/api/requests/' + q.id + '/status', { status: 'done', solution: ta.value }); c(); render(); } catch (e) { fail(e); } } }]);
}
let CONTACT = '';
function requestCard(q) {
  const box = el('div', { class: 'card', style: 'margin-top:12px' },
    el('div', { class: 'card-h' }, el('h3', {}, q.name), q.deviceType ? el('span', { class: 'sub', style: 'background:var(--surface-3);color:var(--fg);font-weight:600;font-size:11.5px;padding:3px 9px;border-radius:999px' }, q.deviceType) : null, pill({ new: 'info', working: 'warn', done: 'ok' }[q.status] || 'off', reqStatusLabel[q.status] || q.status), q.urgent ? pill('crit', 'Gấp') : null, el('span', { class: 'sub' }, '· ' + ago(q.created)), el('div', { style: 'flex:1' }),
      canWrite() ? select([['new', 'Mới'], ['working', 'Đang xử lý'], ['done', 'Đã xong']], q.status, async x => { if (x === 'done') return solveModal(q, ''); try { await api('PUT', '/api/requests/' + q.id + '/status', { status: x }); render(); } catch (e) { fail(e); } }) : null),
    el('div', { class: 'card-b' }, el('div', { style: 'font-size:13px;white-space:pre-wrap' }, q.text),
      (q.reporter || q.phone || q.empId || q.badPc) ? el('div', { style: 'font-size:12px;margin-top:8px;display:flex;flex-wrap:wrap;gap:6px 14px' },
        q.reporter ? el('span', {}, el('b', {}, 'Người báo: '), q.reporter) : null,
        q.phone ? el('span', {}, el('b', {}, (CONTACT || 'SĐT/Zalo') + ': '), /^[+\d][\d .-]{5,}$/.test(q.phone) ? el('a', { href: 'tel:' + q.phone.replace(/[ .-]/g, ''), style: 'color:var(--accent)' }, q.phone) : q.phone) : null,
        q.empId ? el('span', {}, el('b', {}, 'Mã NV: '), q.empId) : null,
        q.badPc ? el('span', {}, el('b', {}, 'Máy hư: '), q.badPc) : null) : null,
      el('div', { style: 'font-size:11.5px;color:var(--faint);margin-top:4px' }, t('Máy gửi: {0} · người dùng {1}', q.name || '—', q.user || '—')),
      (q.replies || []).length ? el('div', { class: 'scrollbox short', style: 'margin-top:10px;display:flex;flex-direction:column;gap:7px' }, q.replies.map(rp => el('div', { style: 'font-size:12.5px;background:var(--surface-2);border-radius:8px;padding:7px 10px' }, el('b', {}, rp.client ? q.name : rp.by), ': ', rp.text, el('span', { style: 'color:var(--faint);margin-left:6px' }, ago(rp.at))))) : null));
  if (q.status === 'done' && q.solution) box.lastChild.append(el('div', { style: 'margin-top:10px;font-size:12.5px;background:var(--good-soft);color:var(--fg);border-radius:8px;padding:8px 10px' }, el('b', {}, 'Cách xử lý: '), q.solution, q.solvedBy ? el('span', { style: 'color:var(--faint)' }, ' — ' + q.solvedBy) : null));
  if (q.status !== 'done' && canWrite()) {
    const sg = el('div', { style: 'margin-top:10px;display:none' });
    box.lastChild.append(sg);
    api('GET', '/api/requests/' + q.id + '/suggest').then(list => {
      if (!list.length) return;
      sg.style.display = 'block';
      sg.append(el('div', { style: 'font-size:12px;font-weight:600;margin-bottom:5px' }, 'Gợi ý từ những lần xử lý trước'),
        ...list.map(h => el('div', { style: 'font-size:12.5px;background:var(--accent-soft);border-radius:8px;padding:8px 10px;margin-bottom:6px' },
          el('div', { style: 'color:var(--muted);font-size:11.5px' }, h.name + ' · ' + h.text),
          el('div', { style: 'margin-top:2px' }, el('b', {}, 'Đã xử lý: '), h.solution),
          el('button', { class: 'btn', style: 'padding:3px 9px;font-size:12px;margin-top:6px', onclick: () => solveModal(q, h.solution) }, 'Dùng cách này & đánh dấu xong'))));
    }).catch(() => {});
  }
  if (canWrite()) {
    const ta = el('textarea', { class: 'input', rows: 2, maxlength: 500, placeholder: 'Trả lời…', style: 'width:100%;margin-top:10px' });
    box.lastChild.append(ta, el('div', { style: 'margin-top:6px' }, el('button', { class: 'btn primary', style: 'padding:4px 10px;font-size:12px', onclick: async () => {
      if (!ta.value.trim()) return;
      try { await api('POST', '/api/requests/' + q.id + '/reply', { text: ta.value }); ta.value = ''; render(); } catch (e) { fail(e); } } }, 'Gửi trả lời')));
  }
  return box;
}
let chatSel = null, chatKeep = null;
const chatLen = {};
const CQ = { q: '' };

// Tin nhắn kiểu Messenger: tin liền nhau của một người gom thành cụm; ảnh tròn và giờ chỉ hiện ở tin cuối cụm.
function chatRows(d) {
  const L = d.lines, key = l => (l.client ? 'm' : 'u') + '|' + (l.byId || l.by), out = [];
  L.forEach((l, i) => {
    const k = key(l), t0 = Date.parse(l.at), first = !(i > 0 && key(L[i - 1]) === k && t0 - Date.parse(L[i - 1].at) < 5 * 60e3);
    const last = !(i < L.length - 1 && key(L[i + 1]) === k && Date.parse(L[i + 1].at) - t0 < 5 * 60e3);
    const inc = l.client, who = inc ? d.name : l.by;
    const av = inc ? (last ? avatarEl('m', d.id, d.av, d.name, 32) : el('span', { class: 'av-sp' })) : null;
    const bub = el('div', { class: 'bub ' + (inc ? 'in' : 'out') + (first ? ' gfirst' : '') }, (first && (inc || l.by !== me.name)) ? el('div', { class: 'bub-w' }, who) : null, el('div', {}, l.text));
    out.push(el('div', { class: 'brow ' + (inc ? 'in' : 'out') + (first ? ' gfirst' : '') }, av, el('div', { class: 'bcol' }, bub, last ? el('div', { class: 'bub-t' }, fmtDT(l.at)) : null)));
  });
  return out;
}
VIEWS.chat = async (v, quiet) => {
  if (me.role === 'view') { v.append(infoNote('Tài khoản Chỉ xem không nhắn tin được.')); return; }
  const [cv, ml] = await Promise.all([api('GET', '/api/chat'), api('GET', '/api/machines')]);
  cv.convs = cv.convs.filter(c => hit(CQ.q, c.name, c.last)).sort((p, q) => String(q.lastAt).localeCompare(String(p.lastAt))); // như Messenger: chỉ tin mới (gửi hoặc nhận) mới đẩy cuộc trò chuyện lên đầu, đọc thì không đổi chỗ
  const sel = chatSel; // không tự mở cuộc nào: mở là đánh dấu đã đọc, phải do người dùng chọn
  const side = el('div', { class: 'chat-side' });
  const picker = el('select', { class: 'input', onchange: e => { if (e.target.value) { chatSel = e.target.value; render(); } } }, el('option', { value: '' }, t('+ Nhắn máy khác…')),
    ...ml.filter(m => m.canControl !== false && m.online).map(m => el('option', { value: m.id }, m.name)));
  side.append(picker, searchBox('cq', CQ.q, x => CQ.q = x, t('Tìm cuộc trò chuyện…')));
  cv.convs.forEach(c => side.append(el('button', { class: 'chat-conv' + (c.id === sel ? ' on' : '') + (c.unread ? ' unread' : ''), onclick: () => { chatSel = c.id; render(); } },
    el('span', { class: 'avwrap' }, avatarEl('m', c.id, c.av, c.name, 34), el('span', { class: 'dot ' + (c.online ? 'on' : '') })), el('div', { style: 'min-width:0;flex:1' }, el('b', {}, c.name), el('div', { class: 'chat-last' }, (c.mine ? t('Bạn: ') : '') + c.last)),
    c.unread ? el('span', { class: 'count badge-red' }, String(c.unread)) : null)));
  const main = el('div', { class: 'chat-main' });
  if (!sel) main.append(el('div', { class: 'empty' }, cv.convs.length ? 'Chọn một cuộc trò chuyện ở bên trái để đọc và trả lời.' : 'Chưa có cuộc trò chuyện nào. Chọn một máy đang online ở ô bên trái để bắt đầu.'));
  else {
    const d = await api('GET', '/api/chat/' + sel);
    const log = el('div', { class: 'chat-log' }, d.lines.length ? chatRows(d) : el('div', { class: 'empty' }, 'Chưa có tin nhắn.'));
    const ta = el('textarea', { class: 'input', rows: 2, maxlength: 300, placeholder: t('Nhập tin nhắn… (Enter để gửi)') });
    const send = async () => { const x = ta.value.trim(); if (!x) return; try { await api('POST', '/api/chat/' + sel, { text: x }); ta.value = ''; render(); } catch (e) { fail(e); } };
    ta.onkeydown = e => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send(); } };
    main.append(el('div', { class: 'chat-head' }, el('b', {}, d.name), el('span', { style: 'color:var(--faint);font-size:12.5px' }, d.online ? 'Online' : 'Offline, chưa gửi được')), log,
      el('div', { class: 'chat-in' }, ta, el('button', { class: 'btn primary', disabled: !d.online, onclick: send }, 'Gửi')));
    // tự kéo xuống cuối khi mở hoặc có tin mới; làm mới định kỳ thì giữ nguyên chỗ đang đọc
    const grew = chatLen[sel] !== d.lines.length; chatLen[sel] = d.lines.length;
    if (!quiet || grew) setTimeout(() => { log.scrollTop = log.scrollHeight; }, 0); else chatKeep = log;
  }
  v.append(el('div', { class: 'chat-wrap' }, side, main));
};
VIEWS.requests = async v => {
  const all = await api('GET', '/api/requests?status=' + RF.st);
  // gấp lên đầu, rồi mới > đang xử lý > đã xong, trong cùng nhóm thì mới nhất trước
  const ord = { new: 0, working: 1, done: 2 };
  const list = all.filter(q => (RF.type === 'all' || (q.deviceType || '') === RF.type) && (!RF.from || dayOf(q.created) >= RF.from) && (!RF.to || dayOf(q.created) <= RF.to) && hit(RF.q, q.name, q.text, q.reporter, q.phone, q.empId)).sort((p, q) => (!!q.urgent) - (!!p.urgent) || ord[p.status] - ord[q.status] || String(q.created).localeCompare(String(p.created)));
  const kinds = [...new Set(all.map(q => q.deviceType || '').filter(Boolean))];
  if (me.role === 'super') {
    const types = await api('GET', '/api/device-types');
    const save = async arr => { try { await api('PUT', '/api/device-types', { types: arr }); render(); } catch (e) { fail(e); } };
    const inp = el('input', { class: 'input', maxlength: 30, placeholder: 'Thêm loại thiết bị, ví dụ: Máy chiếu', style: 'flex:1;min-width:180px' });
    const cl = el('input', { class: 'input', maxlength: 30, placeholder: 'SĐT/Zalo', value: CONTACT === 'SĐT/Zalo' ? '' : CONTACT, style: 'flex:1;min-width:180px' });
    const add = () => { const x = inp.value.trim(); if (x) save(types.concat(x)); };
    inp.onkeydown = e => { if (e.key === 'Enter') add(); };
    v.append(el('div', { class: 'card', style: 'margin-bottom:12px' }, el('div', { class: 'card-h' }, el('h3', {}, 'Loại thiết bị trong yêu cầu hỗ trợ'), el('span', { class: 'sub' }, '· hiện trong ô chọn ở cửa sổ MAM của người dùng')),
      el('div', { class: 'card-b' }, el('div', { style: 'display:flex;flex-wrap:wrap;gap:8px' }, types.map(x => el('span', { style: 'display:inline-flex;align-items:center;gap:6px;background:var(--surface-3);border-radius:999px;padding:5px 6px 5px 12px;font-size:13px;font-weight:600' }, x,
        types.length > 1 ? el('button', { class: 'btn', title: 'Xóa', style: 'padding:0 7px;font-size:13px;line-height:20px;border-radius:999px', onclick: () => save(types.filter(y => y !== x)) }, '×') : null))),
        el('div', { style: 'display:flex;gap:8px;margin-top:12px;flex-wrap:wrap' }, inp, el('button', { class: 'btn primary', onclick: add }, 'Thêm')),
        el('div', { style: 'border-top:1px solid var(--border);margin-top:14px;padding-top:12px' }, el('b', { style: 'font-size:13px' }, 'Tên ô liên hệ của người báo'), el('span', { class: 'sub', style: 'margin-left:8px' }, '· ví dụ: WeChat ID, Telegram. Để trống = SĐT/Zalo'),
          el('div', { style: 'display:flex;gap:8px;margin-top:8px;flex-wrap:wrap' }, cl, el('button', { class: 'btn primary', onclick: async () => { try { CONTACT = (await api('PUT', '/api/contact-label', { label: cl.value })).label; toast('Đã lưu tên ô liên hệ'); render(); } catch (e) { fail(e); } } }, 'Lưu'))))));
  }
  // Thanh lọc: hàng 1 = trạng thái + loại thiết bị + tìm; hàng 2 = ngày gửi (nút nhanh Hôm nay/Hôm qua/7 ngày/30 ngày + khoảng ngày)
  const ymd = d => dayOf(d.toISOString());
  const daysAgo = n => ymd(new Date(Date.now() - n * 864e5));
  const today = ymd(new Date()), yest = daysAgo(1);
  const quick = [['Hôm nay', today, today], ['Hôm qua', yest, yest], ['7 ngày', daysAgo(6), today], ['30 ngày', daysAgo(29), today]];
  const setRange = (f, tt) => { RF.from = f; RF.to = tt; render(); };
  const chip = ([l, f, tt]) => el('button', { class: 'chip' + (RF.from === f && RF.to === tt ? ' on' : ''), onclick: () => RF.from === f && RF.to === tt ? setRange('', '') : setRange(f, tt) }, l);
  v.append(el('div', { class: 'fbar' },
    el('div', { class: 'frow' }, el('div', { class: 'seg' }, [['open', 'Chưa xong'], ['done', 'Đã xong'], ['', 'Tất cả']].map(([k, l]) => el('button', { class: RF.st === k ? 'on' : '', onclick: () => { RF.st = k; render(); } }, l))),
      el('span', { style: 'color:var(--muted);font-size:12.5px' }, t('{0} yêu cầu', list.length)), el('div', { style: 'flex:1' }),
      el('select', { class: 'input', style: 'max-width:200px', onchange: e => { RF.type = e.target.value; render(); } }, el('option', { value: 'all' }, t('Mọi loại thiết bị')), ...kinds.map(k => el('option', { value: k, selected: RF.type === k }, k))),
      searchBox('rfq', RF.q, x => RF.q = x, t('Tìm máy, người báo, nội dung…'))),
    el('div', { class: 'frow fdate' }, el('span', { class: 'flab' }, 'Ngày gửi'), ...quick.map(chip), el('span', { class: 'fsep' }), rangePicker(RF.from, RF.to, setRange),
      RF.from || RF.to ? el('button', { class: 'btn fclr', onclick: () => setRange('', '') }, 'Xóa lọc ngày') : null)));
  if (!list.length) v.append(el('div', { style: 'margin-top:12px' }, el('div', { class: 'empty' }, 'Không có yêu cầu nào')));
  v.append(...list.map(requestCard));
  v.append(el('div', { style: 'margin-top:12px' }, infoNote('Người ngồi máy gửi yêu cầu từ cửa sổ MAM (icon khay hệ thống). Trả lời ở đây sẽ hiện ngay trong cửa sổ đó.')));
};

/* ---------- Kiểm kê ---------- */
function licenseModal(it) {
  const ta = el('textarea', { class: 'input', rows: 5, maxlength: 2000, placeholder: 'Ví dụ:\nWindows 11 Pro — key XXXXX-XXXXX-XXXXX-XXXXX-XXXXX\nMicrosoft 365 — thuê bao công ty, hết hạn 12/2026', style: 'width:100%' }, it.licenseNote || '');
  modal(t('Bản quyền phần mềm · {0}', it.name), ta, [{ label: 'Hủy', on: c => c() }, { label: 'Lưu', cls: 'primary', on: async c => {
    try { await api('PUT', '/api/machines/' + it.id + '/license-note', { note: ta.value }); c(); toast('Đã lưu'); render(); } catch (e) { fail(e); } } }]);
}
const IV = { group: 'all', q: '' };
VIEWS.inventory = async v => {
  const [all, g] = await Promise.all([api('GET', '/api/inventory'), api('GET', '/api/groups')]);
  groups = g.groups.map(x => x.name);
  const q = IV.q.trim().toLowerCase();
  const list = all.filter(it => (IV.group === 'all' || (IV.group === '__none' ? !it.group : it.group === IV.group)) && (!q || [it.name, it.ip, it.os, it.model, it.cpu, it.serial, it.licenseNote].join(' ').toLowerCase().includes(q)));
  v.append(el('div', { class: 'fbar' }, el('div', { class: 'frow' },
    select([['all', 'Tất cả nhóm'], ['__none', 'Chưa phân nhóm'], ...groups.map(x => [x, x])], IV.group, x => { IV.group = x; render(); }),
    el('span', { style: 'color:var(--muted);font-size:12.5px' }, t('{0} máy', list.length)), el('div', { style: 'flex:1' }),
    el('a', { class: 'btn', href: '/api/export/inventory.xlsx' + grpQ(IV.group) }, 'Xuất Excel'), el('a', { class: 'btn', href: '/api/export/inventory.csv' + grpQ(IV.group) }, 'Xuất CSV'),
    searchBox('ivq', IV.q, x => IV.q = x, t('Tìm máy, IP, model, serial, bản quyền…')))));
  v.append(el('div', { class: 'card' }, el('div', { class: 'card-b', style: 'padding:0 16px 8px' }, table(['Máy', 'IP', 'Nhóm', 'Hệ điều hành', 'Model', 'CPU', 'RAM', 'Serial', 'Bản quyền phần mềm', ''], list.map(it => el('tr', { class: 'nohover' },
    el('td', { style: 'font-weight:600' }, it.name), el('td', { class: 'mono' }, it.ip || '—'), el('td', {}, it.group || el('span', { style: 'color:var(--faint)' }, 'Chưa phân nhóm')), el('td', {}, it.os || '—'), el('td', {}, it.model || '—'), el('td', {}, it.cpu || '—'), el('td', { class: 'mono' }, it.ramGB ? it.ramGB + ' GB' : '—'), el('td', { class: 'mono' }, it.serial || '—'),
    el('td', { style: 'max-width:220px;white-space:pre-wrap;font-size:12px;color:var(--muted)' }, it.licenseNote ? it.licenseNote.slice(0, 80) + (it.licenseNote.length > 80 ? '…' : '') : el('span', { style: 'color:var(--faint)' }, 'Chưa ghi')),
    canWrite() ? el('td', { style: 'text-align:right' }, el('button', { class: 'btn', style: 'padding:4px 10px;font-size:12px', onclick: () => licenseModal(it) }, it.licenseNote ? 'Sửa' : '+ Ghi')) : null)), 'Không có máy nào'))));
  v.append(el('div', { style: 'margin-top:12px' }, infoNote('Phần cứng lấy tự động từ agent. Bản quyền phần mềm (key Windows/Office, thuê bao...) do admin tự ghi — MAM không đọc hay kiểm tra key.')));
};

/* ---------- Bảo mật & bản vá ---------- */
VIEWS.security = async v => {
  const list = await api('GET', '/api/security');
  const avPill = x => !x.av ? el('span', { style: 'color:var(--faint)' }, 'Chưa rõ') : /TẮT/.test(x.av) ? pill('crit', x.av) : pill('ok', x.av);
  v.append(el('div', { class: 'card' }, el('div', { class: 'card-b', style: 'padding:0 16px 8px' }, table(['Máy', 'Nhóm', 'Trạng thái', 'Diệt virus', 'Cập nhật diệt virus', 'Windows Update gần nhất'], list.map(x => el('tr', { onclick: () => go('detail', x.id) },
    el('td', { style: 'font-weight:600' }, x.name), el('td', {}, x.group || 'Chưa phân nhóm'), el('td', {}, x.online ? pill('ok', 'Online') : pill('off', 'Offline')),
    el('td', {}, avPill(x)), el('td', { class: 'mono' }, x.avUpdated ? fmtDT(x.avUpdated) : '—'), el('td', { class: 'mono' }, x.lastPatch || '—'))), 'Chưa có máy nào'))));
  v.append(el('div', { style: 'margin-top:12px' }, infoNote('Chỉ xem tình trạng Windows Defender và bản vá Windows Update do agent đọc được, không ép cài hay chạy cập nhật từ xa. Máy dùng diệt virus khác Defender sẽ hiện "Chưa rõ".')));
};

/* ---------- Qua Internet ---------- */
VIEWS.net = async v => {
  v.append(infoNote('MAM Console chạy trong mạng công ty (LAN) theo mặc định. Muốn mở từ nhà hoặc đang đi công tác, chọn một trong các cách dưới — tất cả đều làm trên máy chạy mam-server, không cần đổi gì ở Console hay agent.'));
  v.append(el('div', { style: 'margin-top:12px;display:flex;flex-direction:column;gap:12px' },
    card('Cách 1 — VPN công ty (khuyên dùng)', 'an toàn nhất', el('div', { style: 'font-size:13px;line-height:1.7;color:var(--muted)' }, 'Cài VPN (WireGuard, Tailscale, OpenVPN...) cho máy chạy mam-server và cho máy đại ca. Khi bật VPN, máy đại ca coi như đang ở trong mạng công ty, vào Console bằng địa chỉ IP nội bộ như bình thường. Không mở cổng nào ra Internet.')),
    card('Cách 2 — Mở cổng router (port-forward) + HTTPS', 'cần biết cấu hình router, có rủi ro nếu làm sai', el('div', { style: 'font-size:13px;line-height:1.7;color:var(--muted)' },
      el('p', { style: 'margin:0 0 8px' }, '1. Trên router công ty, chuyển tiếp (port forward) một cổng ngoài (ví dụ 8443) vào địa chỉ IP nội bộ và cổng của mam-server (mặc định 7070).'),
      el('p', { style: 'margin:0 0 8px' }, '2. Bắt buộc chạy HTTPS (không dùng HTTP thuần qua Internet): xin chứng chỉ miễn phí (Let\'s Encrypt) cho một tên miền trỏ về IP công ty, rồi chạy server kèm chứng chỉ:'),
      el('div', { class: 'pre' }, 'mam-server -addr :8443 -cert fullchain.pem -key privkey.pem'),
      el('p', { style: 'margin:8px 0 0' }, '3. Đổi tài khoản Super admin sang mật khẩu thật mạnh, và đặt Mã đăng ký ở mục Cài đặt để máy lạ không tự đăng ký được.'))),
    card('Cách 3 — Dịch vụ tunnel (Cloudflare Tunnel, Tailscale Funnel...)', 'nhanh, không cần mở cổng router', el('div', { style: 'font-size:13px;line-height:1.7;color:var(--muted)' }, 'Các dịch vụ này tự tạo đường hầm HTTPS ra ngoài mà không cần mở cổng. Cài theo hướng dẫn của dịch vụ, trỏ vào cổng mam-server đang chạy nội bộ (mặc định 7070). Đây là dịch vụ của bên thứ ba, cân nhắc trước khi dùng cho dữ liệu công ty.'))));
  v.append(el('div', { style: 'margin-top:12px' }, infoNote('Dù chọn cách nào, luôn bật mật khẩu mạnh cho mọi tài khoản, và cân nhắc đặt Mã đăng ký (mục Cài đặt) để chặn máy lạ tự ghi danh khi Console lộ ra Internet.')));
};

/* ---------- Chính sách ---------- */
VIEWS.policy = async v => {
  const [list, g] = await Promise.all([api('GET', '/api/policies'), api('GET', '/api/groups')]);
  groups = g.groups.map(x => x.name);
  const gsel = select([['', 'Chưa phân nhóm'], ...groups.map(x => [x, x])], VIEWS.policy.cur || '', x => { VIEWS.policy.cur = x; render(); });
  const cur = VIEWS.policy.cur || '';
  const p = list.find(x => x.group === cur) || { group: cur, usb: false, sitesOn: false, sites: [], appsOn: false, apps: [], hoursOn: false, from: '08:00', to: '18:00' };
  const usb = el('input', { type: 'checkbox', checked: p.usb });
  const sitesOn = el('input', { type: 'checkbox', checked: p.sitesOn }), sites = el('textarea', { class: 'input', rows: 4, placeholder: 'facebook.com\nyoutube.com', style: 'width:100%' }, (p.sites || []).join('\n'));
  const appsOn = el('input', { type: 'checkbox', checked: p.appsOn }), apps = el('textarea', { class: 'input', rows: 4, placeholder: 'steam.exe\nleague of legends.exe', style: 'width:100%' }, (p.apps || []).join('\n'));
  const hoursOn = el('input', { type: 'checkbox', checked: p.hoursOn }), from = input('', 'time', p.from || '08:00'), to = input('', 'time', p.to || '18:00');
  const dis = !isSuper();
  [usb, sitesOn, sites, appsOn, apps, hoursOn, from, to].forEach(x => x.disabled = dis);
  v.append(el('div', { class: 'filters' }, el('div', { class: 'grow' }, field('Áp dụng cho nhóm máy', gsel))));
  if (!isSuper()) v.append(infoNote('Chỉ Super admin đổi chính sách. Bạn đang xem.'));
  v.append(el('div', { style: 'margin-top:12px;display:flex;flex-direction:column;gap:12px' },
    card('Khóa ổ USB', 'chặn mọi USB lưu trữ cắm vào máy trong nhóm này', el('label', { class: 'chk' }, usb, 'Bật khóa USB')),
    card('Chặn trang web', 'theo tên miền, mỗi dòng một trang', el('div', { style: 'display:flex;flex-direction:column;gap:8px' }, el('label', { class: 'chk' }, sitesOn, 'Bật chặn web'), sites, el('div', { style: 'font-size:11.5px;color:var(--faint)' }, 'Khi bật, agent chặn bằng bộ lọc DNS ngay trên máy: chặn cả mọi tên miền phụ (m., web., video, ảnh) trên mọi trình duyệt, kèm các máy chủ DNS mã hóa trình duyệt hay dùng để lách. Có tác dụng trên Windows và macOS chạy agent 1.1.43 trở lên; Linux chỉ chặn qua file hosts. Trình duyệt đang mở cần tải lại trang.'))),
    card('Chặn ứng dụng', 'theo tên tiến trình .exe, mỗi dòng một ứng dụng', el('div', { style: 'display:flex;flex-direction:column;gap:8px' }, el('label', { class: 'chk' }, appsOn, 'Bật chặn ứng dụng'), apps)),
    card('Giờ được dùng máy', 'ngoài khoảng này máy tự khóa', el('div', { style: 'display:flex;flex-direction:column;gap:8px' }, el('label', { class: 'chk' }, hoursOn, 'Bật giới hạn giờ'), el('div', { style: 'display:flex;gap:10px;align-items:center' }, field('Từ', from), field('Đến', to)))),
    isSuper() ? el('div', {}, el('button', { class: 'btn primary', onclick: async () => {
      const sl = sites.value.split('\n'), al = apps.value.split('\n');
      try { await api('PUT', '/api/policies', { group: cur, usb: usb.checked, sitesOn: sitesOn.checked, sites: sl, appsOn: appsOn.checked, apps: al, hoursOn: hoursOn.checked, from: from.value, to: to.value }); toast('Đã lưu chính sách'); render(); } catch (e) { fail(e); } } }, 'Lưu chính sách')) : null));
  v.append(el('div', { style: 'margin-top:12px' }, infoNote('Chính sách gửi tới máy mỗi lần agent báo cáo (vài giây). Chưa đặt chính sách cho nhóm nào = nhóm đó không bị hạn chế.')));
};

/* ---------- Phát hiện máy trong LAN ---------- */
const kindIcon = { Windows: '🖥️', 'iPhone/iPad': '📱', 'Linux/macOS': '🐧', 'Thiết bị mạng': '📶', Khác: '❓' };
VIEWS.discover = async v => {
  const sg = await api('GET', '/api/discover');
  const cidr = input('ví dụ 192.168.1.0/24', 'text', VIEWS.discover.cidr || (sg.suggest && sg.suggest[0]) || '');
  const btn = el('button', { class: 'btn primary', onclick: scan }, 'Quét mạng');
  const box = el('div', { style: 'margin-top:12px' });
  v.append(el('div', { class: 'filters' }, field('Dải mạng nội bộ (tối đa 1.024 địa chỉ)', cidr), btn));
  if (sg.suggest && sg.suggest.length) v.append(el('div', { style: 'font-size:12px;color:var(--muted);margin-top:4px' }, t('Gợi ý theo máy server: {0}', sg.suggest.join(', '))));
  v.append(box);
  async function scan() {
    VIEWS.discover.cidr = cidr.value;
    btn.disabled = true; btn.textContent = t('Đang quét… (tối đa 40 giây)');
    box.innerHTML = '';
    try {
      const r = await api('POST', '/api/discover', { cidr: cidr.value });
      const foundRow = f => el('tr', { class: 'nohover' },
        el('td', {}, kindIcon[f.kind] || '❓'), el('td', { class: 'mono' }, f.ip), el('td', {}, f.host || '—'), el('td', { class: 'mono' }, f.ports.join(', ')),
        el('td', {}, f.agent ? pill('ok', f.agent) : pill('warn', 'Chưa')));
      const tb = table(['', 'IP', 'Tên máy (DNS)', 'Cổng mở', 'Đã cài MAM?'], r.found.map(foundRow), 'Không thấy thiết bị nào phản hồi');
      box.append(el('div', { style: 'margin:10px 0;color:var(--muted);font-size:12.5px' }, t('Đã quét {0} địa chỉ trong {1}, thấy {2} thiết bị có phản hồi.', r.scanned, r.cidr, r.found.length) + (r.partial ? ' ' + t('(hết thời gian, có thể chưa quét xong)') : '')));
      box.append(card('Kết quả', null, tb));
    } catch (e) { fail(e); } finally { btn.disabled = false; btn.textContent = t('Quét mạng'); }
  }
  v.append(el('div', { style: 'margin-top:12px' }, infoNote('Dò cổng TCP thường gặp (135, 445, 3389, 22, 80, 443, 5900, 62078) để đoán có máy đang chạy, không đọc được nội dung máy. Dùng để tìm máy chưa cài MAM Agent trong mạng nội bộ.')));
};

/* ---------- Báo cáo ---------- */
const hesc = x => String(x == null ? '' : x).replace(/[&<>"]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]));
// Xuất PDF: mở trang A4 đã dàn trang sẵn rồi gọi hộp thoại in (chọn "Lưu thành PDF").
const grpQ = g => g && g !== 'all' ? '?group=' + encodeURIComponent(g) : '';
const grpLabel = g => g === '__none' ? t('Chưa phân nhóm') : g;
async function printReport(r, days, group) {
  const gm = grpQ(group) ? await api('GET', '/api/machines') : null;
  const ids = gm ? new Set(gm.filter(m => group === '__none' ? !m.group : m.group === group).map(m => m.id)) : null;
  let [al, rq] = await Promise.all([api('GET', '/api/alerts?status=open'), api('GET', '/api/requests?status=')]);
  if (ids) { al = al.filter(a => ids.has(a.machine)); rq = rq.filter(q => ids.has(q.machine)); }
  const w = window.open('', '_blank');
  if (!w) { toast('Trình duyệt chặn cửa sổ in. Hãy cho phép cửa sổ bật lên rồi thử lại.', 'err'); return; }
  const st = { new: ['Mới', '#E8F0FE', '#1556c0'], working: ['Đang xử lý', '#FDF0D5', '#946006'], done: ['Đã xong', '#DDF3E6', '#177a44'] };
  const now = new Date(r.to + 'T00:00'), from = new Date(r.from + 'T00:00'), f = d => d.toLocaleDateString('vi-VN');
  const kp = [['Tổng số máy', r.machines, t('{0} đang online', r.online), '#0E2A4D'], ['Cảnh báo trong kỳ', r.alerts, t('{0} đang mở', r.alertsOpen), '#b42323'], ['Đóng cảnh báo TB', r.avgCloseMin ? t('{0} phút', Math.round(r.avgCloseMin)) : '—', '', '#177a44'], ['Yêu cầu hỗ trợ', r.requests, t('{0} đã xong', r.requestsDone), '#1556c0']];
  w.document.write(`<!doctype html><html lang="${MAMI.lang() === 'zh' ? 'zh-CN' : MAMI.lang()}"><head><meta charset="utf-8"><title>MAM - ${t('Báo cáo {0} ngày', days)}</title><style>
@page{size:A4;margin:0}*{box-sizing:border-box}body{margin:0;font-family:"Be Vietnam Pro","Segoe UI",system-ui,sans-serif;color:#172030;-webkit-print-color-adjust:exact;print-color-adjust:exact}
.hd{background:#0E2A4D;color:#fff;padding:36px 48px 30px;display:flex;justify-content:space-between;align-items:flex-end}.hd small{letter-spacing:.14em;text-transform:uppercase;color:#9CC4FF;font-weight:600;font-size:12px}.hd h1{margin:6px 0 4px;font-size:28px}.hd p{margin:0;color:#C7D7EE;font-size:13px}.hd .r{text-align:right;font-size:12px;color:#C7D7EE;line-height:1.6}
.bd{padding:26px 48px}.k{display:grid;grid-template-columns:repeat(4,1fr);gap:12px}.k div{border:1px solid #DCE4EE;border-radius:12px;padding:14px 16px}.k small{font-size:11px;font-weight:600;color:#5a6676}.k b{display:block;font-size:26px;margin:6px 0 2px}.k span{font-size:11px;color:#8b97a7}
h2{font-size:14px;color:#0E2A4D;margin:26px 0 10px;padding-bottom:8px;border-bottom:2px solid #1f6feb}table{width:100%;border-collapse:collapse;font-size:11.5px}th{background:#F1F5FA;text-align:left;padding:8px 10px;color:#46515f}td{padding:8px 10px;border-bottom:1px solid #E6ECF3;vertical-align:top}
.p{font-size:10.5px;font-weight:700;padding:2px 8px;border-radius:999px;white-space:nowrap}.fx{color:#177a44;font-size:11.5px;margin-top:3px}.ft{padding:14px 48px;font-size:10.5px;color:#8b97a7}
button{position:fixed;right:20px;top:20px;padding:10px 18px;border:0;border-radius:10px;background:#1f6feb;color:#fff;font:600 14px inherit;cursor:pointer}@media print{button{display:none}}
</style></head><body><button onclick="print()">${t('In / Lưu PDF')}</button>
<div class="hd"><div><small>MAM Console</small><h1>${t('Báo cáo {0} ngày', days)}</h1><p>${t('{0} đến {1}', f(from), f(now))}${group && group !== 'all' ? ' · ' + hesc(t('Nhóm:')) + ' ' + hesc(grpLabel(group)) : ''}</p></div><div class="r">${t('Xuất lúc')} ${now.toLocaleString(MAMI.lang() === 'zh' ? 'zh-CN' : MAMI.lang() === 'en' ? 'en-GB' : 'vi-VN')}<br>${t('Người xuất:')} ${hesc(me.name)}</div></div>
<div class="bd"><div class="k">${kp.map(k => `<div><small>${hesc(t(k[0]))}</small><b style="color:${k[3]}">${hesc(k[1])}</b><span>${hesc(k[2])}</span></div>`).join('')}</div>
<h2>${t('Cảnh báo đang mở')}</h2>${al.length ? `<table><tr><th>${t('Máy')}</th><th>${t('Nội dung')}</th><th>${t('Mở lúc')}</th></tr>${al.map(a => `<tr><td><b>${hesc(a.name)}</b></td><td>${hesc(a.msg)}</td><td>${hesc(new Date(a.opened).toLocaleString(MAMI.lang() === 'zh' ? 'zh-CN' : MAMI.lang() === 'en' ? 'en-GB' : 'vi-VN'))}</td></tr>`).join('')}</table>` : `<p style="font-size:12px;color:#5a6676">${t('Không có cảnh báo đang mở.')}</p>`}
<h2>${t('Yêu cầu hỗ trợ và cách xử lý')}</h2>${rq.slice(0, 15).map(q => { const s = st[q.status] || st.new; return `<table style="margin-bottom:8px"><tr><td><b>${hesc(q.name)}</b> <span style="color:#8b97a7">· ${hesc(t(q.deviceType || 'Không rõ loại'))}</span> <span class="p" style="float:right;background:${s[1]};color:${s[2]}">${t(s[0])}</span><div>${hesc(q.text)}</div>${q.solution ? `<div class="fx"><b>${t('Cách xử lý:')}</b> ${hesc(q.solution)}</div>` : ''}</td></tr></table>`; }).join('') || `<p style="font-size:12px;color:#5a6676">${t('Chưa có yêu cầu.')}</p>`}
</div><div class="ft">${t('Bản quyền © 2026 Nguyễn Tấn Tài (Missing)')}</div></body></html>`);
  w.document.close();
}
// Lịch chọn khoảng ngày (thay <input type=date>): bấm 2 ngày = 1 khoảng, có mốc nhanh. Ngày dạng 'YYYY-MM-DD' theo giờ máy.
const isoL = d => new Date(d.getTime() - d.getTimezoneOffset() * 6e4).toISOString().slice(0, 10);
const pdate = s => { const [y, m, d] = s.split('-').map(Number); return new Date(y, m - 1, d); };
const fmtVN = s => s ? s.split('-').reverse().join('/') : '';
function rangePicker(from, to, apply, opts) {
  opts = opts || {};
  const today = isoL(new Date()), ago = n => isoL(new Date(Date.now() - n * 864e5));
  const presets = [['Hôm nay', today, today], ['Hôm qua', ago(1), ago(1)], ['7 ngày qua', ago(6), today], ['30 ngày qua', ago(29), today], ['Tháng này', today.slice(0, 8) + '01', today]];
  let s = from || today, e = to || s, picking = false, pop = null;
  let vm = pdate(e); vm.setDate(1);
  const btn = el('button', { class: 'input rp-btn', type: 'button', onclick: () => pop ? close() : open() }, from ? fmtVN(from) + ' → ' + fmtVN(to) : to ? t('Đến hết {0}', fmtVN(to)) : 'Chọn khoảng ngày');
  const wrap = el('div', { class: 'rp' }, btn);
  const out = ev => { if (!wrap.contains(ev.target)) close(); }, key = ev => { if (ev.key === 'Escape') close(); };
  function close() { if (pop) { pop.remove(); pop = null; document.removeEventListener('mousedown', out, true); document.removeEventListener('keydown', key, true); } }
  function open() { pop = el('div', { class: 'rp-pop' }); wrap.append(pop); document.addEventListener('mousedown', out, true); document.addEventListener('keydown', key, true); draw(); const r = pop.getBoundingClientRect(), over = r.right - (innerWidth - 12); if (over > 0) pop.style.left = Math.max(-over, 12 - r.left) + 'px'; }
  const nav = (d, label, path) => el('button', { class: 'rp-nav', type: 'button', 'aria-label': label, html: '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><path d="' + path + '"/></svg>', onclick: () => { vm.setMonth(vm.getMonth() + d); draw(); } });
  function draw() {
    const y = vm.getFullYear(), m = vm.getMonth(), off = (new Date(y, m, 1).getDay() + 6) % 7, n = Math.ceil((off + new Date(y, m + 1, 0).getDate()) / 7) * 7;
    const cells = [];
    for (let i = 0; i < n; i++) {
      const dt = new Date(y, m, 1 - off + i), k = isoL(dt), inR = k >= s && k <= e, end = k === s || k === e, dis = opts.max && k > opts.max;
      const band = 'rp-c' + (inR ? ' in' : '') + (k === s ? ' first' : '') + (k === e ? ' last' : '');
      cells.push(el('div', { class: band }, el('button', { type: 'button', disabled: dis, class: 'rp-d' + (end ? ' end' : '') + (dt.getMonth() !== m ? ' out' : '') + (k === today && !end ? ' today' : ''), onclick: () => { if (!picking) { s = e = k; picking = true; } else { if (k < s) { e = s; s = k; } else e = k; picking = false; } draw(); } }, String(dt.getDate()))));
    }
    const days = Math.round((pdate(e) - pdate(s)) / 864e5) + 1;
    pop.replaceChildren(
      el('div', { class: 'rp-main' },
        el('div', { class: 'rp-side' }, ...presets.filter(p => !opts.max || p[1] <= opts.max).map(([l, f, tt]) => el('button', { type: 'button', class: s === f && e === tt ? 'on' : '', onclick: () => { s = f; e = tt; picking = false; vm = pdate(tt); vm.setDate(1); draw(); } }, l))),
        el('div', { class: 'rp-cal' },
          el('div', { class: 'rp-head' }, nav(-1, 'Tháng trước', 'M15 5l-7 7 7 7'), el('b', null, t('Tháng {0}, {1}', m + 1, y)), nav(1, 'Tháng sau', 'M9 5l7 7-7 7')),
          el('div', { class: 'rp-wd' }, ...['T2', 'T3', 'T4', 'T5', 'T6', 'T7', 'CN'].map((w, i) => el('span', { class: i > 4 ? 'we' : '' }, w))),
          el('div', { class: 'rp-grid' }, ...cells))),
      el('div', { class: 'rp-foot' }, el('div', { class: 'rp-sum' }, el('b', null, fmtVN(s) + ' → ' + fmtVN(e)), ' · ' + t('{0} ngày', days)),
        opts.clearable === false ? null : el('button', { type: 'button', class: 'btn fclr', onclick: () => { close(); apply('', ''); } }, 'Xóa lọc ngày'),
        el('button', { type: 'button', class: 'btn primary', onclick: () => { close(); apply(s, e); } }, 'Áp dụng')));
  }
  return wrap;
}
/* ---------- Sao lưu sang ổ khác + ổ cứng máy chủ ---------- */
async function mirrorCard() {
  const st = await api('GET', '/api/backups/mirror');
  const inp = el('input', { class: 'input', id: 'bkdir', placeholder: 'D:\\MAM-backup hoặc \\\\NAS\\MAM-backup', value: st.dir || '', style: 'flex:1;min-width:220px' });
  const go2 = async body => { try { await api('POST', '/api/backups/mirror', body); toast('Đã sao lưu'); } catch (e) { fail(e); } render(); };
  const stat = !st.dir ? infoNote('Chưa bật. Bản sao lưu hằng ngày đang nằm cùng ổ với dữ liệu: ổ máy chủ hỏng là mất cả hai. Hãy chọn một thư mục ở ổ khác, USB hoặc NAS.')
    : !st.at || new Date(st.at).getFullYear() < 2000 ? el('div', { style: 'color:var(--muted)' }, 'Chưa sao lưu lần nào, bản đầu tiên sẽ chạy trong vài phút.')
    : st.ok ? el('div', {}, pill('ok', 'Hoạt động tốt'), ' ', t('Lần cuối {0} · {1} · {2}', fmtDT(st.at), st.file, st.size < 1e6 ? Math.ceil(st.size / 1024) + ' KB' : (st.size / 1e6).toFixed(1) + ' MB'))
    : el('div', {}, pill('crit', 'Lỗi'), ' ', el('span', { style: 'color:var(--crit)' }, t('Lần cuối {0}: {1}', fmtDT(st.at), st.err)));
  const disks = (st.disk || []).map(d => el('div', { class: 'srow' }, el('span', {}, d.name),
    el('b', {}, d.totalGB ? t('{0} / {1} GB trống', num(d.freeGB, 0), num(d.totalGB, 0)) : [/^healthy$/i.test(d.health) ? pill('ok', d.health) : pill('crit', d.health || 'Không rõ'), d.wear ? ' · ' + t('mòn {0}%', num(d.wear, 0)) : '', d.readErr ? ' · ' + t('{0} lỗi đọc', d.readErr) : ''])));
  return card('Sao lưu sang ổ khác', 'mỗi giờ, giữ 24 bản theo giờ, 7 theo ngày, 4 theo tuần, 12 theo tháng', el('div', { style: 'display:flex;flex-direction:column;gap:12px' },
    el('div', { style: 'display:flex;gap:8px;flex-wrap:wrap' }, inp, el('button', { class: 'btn primary', onclick: () => go2({ dir: inp.value }) }, 'Lưu và sao lưu ngay'), st.dir ? el('button', { class: 'btn', onclick: () => go2({ run: true }) }, 'Sao lưu ngay') : null, st.dir ? el('button', { class: 'btn', onclick: () => go2({ dir: '' }) }, 'Tắt') : null),
    stat,
    el('div', { style: 'font-size:12px;color:var(--faint)' }, 'Mỗi bản là một file zip gồm toàn bộ dữ liệu (máy, tài khoản, cài đặt, lịch sử bật máy, lỗi của máy, nhật ký thao tác, ảnh đại diện) và được mở lại kiểm tra ngay sau khi ghi. Sao lưu lỗi thì có cảnh báo đỏ. Thư mục mạng (NAS) phải cho tài khoản chạy dịch vụ MAM ghi vào. Khôi phục: dừng dịch vụ MAM, giải nén file zip vào thư mục dữ liệu, chạy lại.'),
    el('div', {}, el('div', { style: 'font-weight:600;margin-bottom:4px' }, 'Ổ cứng máy chủ'), disks.length ? disks : el('div', { style: 'color:var(--faint)' }, 'Đang kiểm tra…'),
      el('div', { style: 'font-size:12px;color:var(--faint);margin-top:6px' }, st.diskAt && new Date(st.diskAt).getFullYear() > 2000 ? t('Kiểm tra mỗi 6 giờ, lần cuối {0}. Ổ có dấu hiệu hỏng hoặc sắp đầy thì có cảnh báo.', fmtDT(st.diskAt)) : 'Kiểm tra mỗi 6 giờ. Ổ có dấu hiệu hỏng hoặc sắp đầy thì có cảnh báo.'))));
}
/* ---------- Thời gian hoạt động (máy bật lúc nào, bao lâu) ---------- */
// Server trả các lần bật [bắt đầu, kết thúc] (giây unix); chia theo ngày bằng giờ của trình duyệt.
const UP = { per: 'day', ref: null };
const DOWS = ['CN', 'T2', 'T3', 'T4', 'T5', 'T6', 'T7'];
const upAdd = (d, n) => new Date(d.getFullYear(), d.getMonth(), d.getDate() + n);
const upMon = d => upAdd(d, -((d.getDay() + 6) % 7));
const upDD = d => pad(d.getDate()) + '/' + pad(d.getMonth() + 1);
const upISO = d => d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate());
const upClock = m => m >= 1440 ? '24:00' : pad(Math.floor(m / 60)) + ':' + pad(Math.floor(m % 60));
const upH = m => t('{0}g', (m / 60).toFixed(1));
function upDur(min) { const m = Math.round(min), h = Math.floor(m / 60); return h ? (m % 60 ? t('{0} giờ {1} phút', h, m % 60) : t('{0} giờ', h)) : t('{0} phút', m); }
function upToday() { const n = new Date(); return new Date(n.getFullYear(), n.getMonth(), n.getDate()); }
function upDays() {
  const r = UP.ref || (UP.ref = upToday());
  if (UP.per === 'day') return [r];
  if (UP.per === 'week') { const m = upMon(r); return [0, 1, 2, 3, 4, 5, 6].map(i => upAdd(m, i)); }
  const out = [], y = r.getFullYear();
  for (let d = UP.per === 'month' ? new Date(y, r.getMonth(), 1) : new Date(y, 0, 1); UP.per === 'month' ? d.getMonth() === r.getMonth() : d.getFullYear() === y; d = upAdd(d, 1)) out.push(d);
  return out;
}
function upLabel() {
  const r = UP.ref || upToday();
  if (UP.per === 'day') return (+r === +upToday() ? t('Hôm nay') : t(DOWS[r.getDay()])) + ', ' + upDD(r) + '/' + r.getFullYear();
  if (UP.per === 'week') { const m = upMon(r), e = upAdd(m, 6); return upDD(m) + ' – ' + upDD(e) + '/' + e.getFullYear(); }
  if (UP.per === 'month') return t('Tháng {0}, {1}', r.getMonth() + 1, r.getFullYear());
  return t('Năm {0}', r.getFullYear());
}
function upShift(n) {
  const r = UP.ref || upToday();
  UP.ref = UP.per === 'day' ? upAdd(r, n) : UP.per === 'week' ? upAdd(r, 7 * n) : UP.per === 'month' ? new Date(r.getFullYear(), r.getMonth() + n, 1) : new Date(r.getFullYear() + n, 0, 1);
  if (UP.ref > upToday()) UP.ref = upToday();
  render();
}
// Thanh chọn kỳ: Ngày / Tuần / Tháng / Năm và nút lùi/tới
function upBar(...extra) {
  const ds = upDays(), atEnd = ds.some(d => +d === +upToday());
  return el('div', { class: 'filters' },
    el('div', { class: 'seg' }, [['day', 'Ngày'], ['week', 'Tuần'], ['month', 'Tháng'], ['year', 'Năm']].map(([k, l]) => el('button', { class: UP.per === k ? 'on' : '', onclick: () => { UP.per = k; render(); } }, l))),
    el('div', { class: 'up-nav' }, el('button', { class: 'btn', 'aria-label': 'Kỳ trước', onclick: () => upShift(-1) }, '‹'), el('b', {}, upLabel()), el('button', { class: 'btn', 'aria-label': 'Kỳ sau', disabled: atEnd, onclick: () => upShift(1) }, '›')),
    el('div', { style: 'flex:1' }), ...extra);
}
async function upFetch(q) {
  const ds = upDays(), from = ds[0].getTime() / 1000, to = upAdd(ds[ds.length - 1], 1).getTime() / 1000;
  const r = await api('GET', '/api/uptime?from=' + from + '&to=' + to + q);
  // lần bật còn mới (agent vẫn đang báo) thì kéo tới hiện tại và đánh dấu đang bật
  r.machines.forEach(m => { m.s = m.s.map(x => r.now - x[1] <= r.gap ? [x[0], r.now, 1] : [x[0], x[1], 0]); });
  r.nowMs = r.now * 1000;
  return r;
}
// các lần bật trong ngày d, tính bằng phút từ 0h: [bắt đầu, kết thúc, đang bật]
function upDaySes(S, d) {
  const a = d.getTime() / 1000, b = upAdd(d, 1).getTime() / 1000, out = [];
  for (const x of S) { const s = Math.max(x[0], a), e = Math.min(x[1], b); if (e > s) out.push([(s - a) / 60, (e - a) / 60, x[2] && e === x[1]]); }
  return out;
}
const upSum = ss => ss.reduce((a, s) => a + s[1] - s[0], 0);
function upStats(S, nowMs) {
  const ds = upDays().map(d => ({ d, fut: d.getTime() > nowMs, ss: upDaySes(S, d) }));
  const used = ds.filter(x => x.ss.length), tot = ds.reduce((a, x) => a + upSum(x.ss), 0);
  return { ds, used, tot, firsts: used.map(x => x.ss[0][0]), lasts: used.map(x => x.ss[x.ss.length - 1][1]), live: S.some(x => x[2]) };
}
const upAvg = a => a.length ? upClock(Math.round(a.reduce((x, y) => x + y, 0) / a.length)) : '—';
// thanh 24 giờ nhỏ (dùng trong bảng)
function upStrip(ss) {
  let o = '<rect x="0" y="7" width="1440" height="2" fill="var(--border)"/>' + [360, 720, 1080].map(x => `<rect x="${x}" y="0" width="3" height="16" fill="var(--border)"/>`).join('');
  ss.forEach(s => o += `<rect class="up-ses${s[2] ? ' live' : ''}" x="${s[0]}" y="2" width="${Math.max(6, s[1] - s[0])}" height="12" rx="4"><title>${upClock(s[0])} – ${s[2] ? t('đang bật') : upClock(s[1])}</title></rect>`);
  return el('div', { class: 'up-strip', html: `<svg viewBox="0 0 1440 16" preserveAspectRatio="none">${o}</svg>` });
}
// trục 24 giờ lớn của một ngày
function upTimeline(ss) {
  const W = 960, L = 10, x = m => L + m / 1440 * (W - 2 * L);
  let o = `<rect x="${L}" y="22" width="${W - 2 * L}" height="22" rx="6" fill="var(--surface-2)"/>`;
  for (let h = 0; h <= 24; h += 2) o += `<line class="up-grid" x1="${x(h * 60)}" x2="${x(h * 60)}" y1="14" y2="52"/><text x="${x(h * 60)}" y="68" text-anchor="middle">${h}h</text>`;
  ss.forEach(s => {
    o += `<rect class="up-ses${s[2] ? ' live' : ''}" x="${x(s[0])}" y="22" width="${Math.max(3, x(s[1]) - x(s[0]))}" height="22" rx="6"><title>${upClock(s[0])} – ${s[2] ? t('đang bật') : upClock(s[1])} (${upDur(s[1] - s[0])})</title></rect>`;
    if (x(s[1]) - x(s[0]) > 80) o += `<text class="up-in" x="${(x(s[0]) + x(s[1])) / 2}" y="37" text-anchor="middle">${upDur(s[1] - s[0])}</text>`;
  });
  return el('div', { class: 'up-chart', html: `<svg viewBox="0 0 ${W} 74">${o}</svg>` });
}
// biểu đồ cột tổng giờ: items [{lb, sub, v (phút), dim}]
// n = true: cột là số lần (lỗi), không phải phút
function upBars(items, n) {
  const W = 960, H = 230, L = 34, B = 40, T = 22, max = Math.max(n ? 4 : 60, ...items.map(i => i.v));
  const p10 = 10 ** Math.floor(Math.log10(max / 4)), q = max / 4 / p10;
  const step = n ? Math.max(1, (q <= 1 ? 1 : q <= 2 ? 2 : q <= 5 ? 5 : 10) * p10) : max > 6000 ? 3000 : max > 2400 ? 1200 : max > 900 ? 240 : 120, top = Math.ceil(max / step) * step;
  const y = v => H - B - v / top * (H - B - T), bw = (W - L) / items.length;
  let o = '';
  for (let v = 0; v <= top; v += step) o += `<line class="up-grid" x1="${L}" x2="${W}" y1="${y(v)}" y2="${y(v)}"/><text x="${L - 6}" y="${y(v) + 4}" text-anchor="end">${n ? v : t('{0}g', v / 60)}</text>`;
  items.forEach((it, i) => {
    const cx = L + bw * i + bw / 2, w = Math.min(46, bw * .62), hg = Math.max(it.v ? 2 : 0, H - B - y(it.v));
    o += `<rect class="up-b${it.dim ? ' dim' : ''}" x="${cx - w / 2}" y="${H - B - hg}" width="${w}" height="${hg}" rx="4"><title>${it.lb} ${it.sub || ''}: ${n ? t('{0} lỗi', it.v) : upDur(it.v)}</title></rect>`;
    if (it.v && bw > 26) o += `<text class="up-val" x="${cx}" y="${H - B - hg - 6}" text-anchor="middle">${n ? it.v : upH(it.v)}</text>`;
    o += `<text class="up-lb" x="${cx}" y="${H - B + 16}" text-anchor="middle">${it.lb}</text>`;
    if (it.sub) o += `<text x="${cx}" y="${H - B + 31}" text-anchor="middle">${it.sub}</text>`;
  });
  return el('div', { class: 'up-chart', html: `<svg viewBox="0 0 ${W} ${H}">${o}</svg>` });
}
// Tab "Thời gian hoạt động" trong chi tiết máy
async function upDetail(id) {
  const r = await upFetch('&id=' + encodeURIComponent(id)), S = r.machines[0].s, st = upStats(S, r.nowMs), one = UP.per === 'day';
  const wrap = el('div', { style: 'display:flex;flex-direction:column;gap:12px' }, upBar());
  const ss = one ? st.ds[0].ss : [], last = ss[ss.length - 1];
  const offs = ss.slice(1).reduce((a, s, i) => a + s[0] - ss[i][1], 0);
  wrap.append(el('div', { class: 'kpis up-kpis' }, one ? [
    kpi('Tổng thời gian bật', 'var(--accent)', upDur(st.tot), last && last[2] ? t('đang bật') : ''),
    kpi('Bật lúc', 'var(--good)', ss.length ? upClock(ss[0][0]) : '—', 'Lần bật đầu tiên trong ngày'),
    kpi('Tắt lúc', 'var(--muted)', !last ? '—' : last[2] ? t('Chưa tắt') : upClock(last[1]), 'Lần tắt cuối cùng'),
    kpi('Số lần bật', 'var(--warn)', ss.length, ss.length > 1 ? t('Tắt giữa chừng {0} lần, tổng {1}', ss.length - 1, upDur(offs)) : ss.length ? 'Bật liên tục' : '')] : [
    kpi('Tổng thời gian bật', 'var(--accent)', upDur(st.tot), t('{0} ngày có bật máy', st.used.length)),
    kpi('Trung bình mỗi ngày', 'var(--good)', st.used.length ? upDur(st.tot / st.used.length) : '—', 'Tính trên ngày có bật máy'),
    kpi('Bật trung bình lúc', 'var(--muted)', upAvg(st.firsts), t('Sớm nhất {0}', st.firsts.length ? upClock(Math.min(...st.firsts)) : '—')),
    kpi('Tắt trung bình lúc', 'var(--warn)', upAvg(st.lasts), t('Muộn nhất {0}', st.lasts.length ? upClock(Math.max(...st.lasts)) : '—'))]));
  if (one) {
    wrap.append(card('Các khoảng giờ hoạt động', 'rê chuột lên từng khúc để xem giờ', upTimeline(ss)),
      card('Chi tiết từng lần bật', '', ss.length ? el('div', { class: 'up-list' }, ss.flatMap((s, i) => [
        i ? el('div', { class: 'gap' }, el('span', {}, s[0] - ss[i - 1][1] <= 30 ? 'Khởi động lại' : 'Tắt máy'), el('span', { class: 'mono' }, t('nghỉ {0}', upDur(s[0] - ss[i - 1][1])))) : null,
        el('div', {}, el('span', {}, t('Lần {0}', i + 1) + ': ', el('b', { class: 'mono' }, upClock(s[0]) + ' – ' + (s[2] ? t('nay') : upClock(s[1])))), el('span', { class: 'mono' }, upDur(s[1] - s[0])))])) : el('div', { class: 'empty' }, 'Máy không bật trong ngày này')));
  } else if (UP.per === 'week') {
    wrap.append(card('Tổng giờ từng ngày trong tuần', '', upBars(st.ds.map(x => ({ lb: t(DOWS[x.d.getDay()]), sub: upDD(x.d), v: upSum(x.ss), dim: x.fut })))),
      card('Khoảng giờ bật máy từng ngày', 'mỗi hàng là 24 giờ, vạch mờ là 6h, 12h, 18h', table(['Ngày', 'Các khoảng giờ bật (0h – 24h)', { t: 'Tổng giờ', cls: 'r' }], st.ds.map(x => el('tr', { class: 'nohover' }, el('td', { style: 'white-space:nowrap' }, t(DOWS[x.d.getDay()]) + ' ' + upDD(x.d)), el('td', { style: 'width:100%' }, x.fut ? el('span', { style: 'color:var(--faint)' }, 'Chưa tới') : upStrip(x.ss)), el('td', { class: 'r mono' }, x.fut ? '' : upDur(upSum(x.ss))))))));
  } else if (UP.per === 'month') {
    const wk = []; st.ds.forEach(x => { const k = +upMon(x.d); let w = wk.find(w => w.k === k); if (!w) wk.push(w = { k, ds: [] }); w.ds.push(x); });
    wrap.append(card('Tổng giờ từng tuần trong tháng', '', upBars(wk.map((w, i) => ({ lb: t('Tuần {0}', i + 1), sub: upDD(w.ds[0].d) + '–' + upDD(w.ds[w.ds.length - 1].d), v: w.ds.reduce((a, x) => a + upSum(x.ss), 0), dim: w.ds[0].fut })))),
      card('Lịch tháng', 'màu đậm là bật lâu', el('div', { class: 'up-cal' }, ['T2', 'T3', 'T4', 'T5', 'T6', 'T7', 'CN'].map(x => el('span', {}, x)),
        Array.from({ length: (st.ds[0].d.getDay() + 6) % 7 }, () => el('div', { class: 'x' })),
        st.ds.map(x => { const v = upSum(x.ss), dark = v > 360; return el('div', { title: upDD(x.d) + ': ' + (x.fut ? t('Chưa tới') : upDur(v)), class: dark ? 'dk' : '', style: v ? `background:color-mix(in srgb,var(--accent) ${15 + Math.min(85, v / 720 * 85) | 0}%,var(--surface-2))` : '' }, el('span', {}, String(x.d.getDate())), el('b', {}, v ? upH(v) : '')); }))));
  } else {
    wrap.append(card('Tổng giờ từng tháng trong năm', '', upBars(Array.from({ length: 12 }, (_, i) => { const ds = st.ds.filter(x => x.d.getMonth() === i); return { lb: t('T{0}', i + 1), v: ds.reduce((a, x) => a + upSum(x.ss), 0), dim: ds[0].fut }; }))));
  }
  wrap.append(el('div', { style: 'font-size:12px;color:var(--faint)' }, 'Máy tính là đang bật khi agent còn báo về server. Mất liên lạc quá 2 phút là tắt; khởi động lại được tách thành lần bật mới. Dữ liệu có từ khi cài bản 1.1.47 và giữ 1 năm.'));
  return wrap;
}
/* ---------- Lỗi của máy (180 ngày) ---------- */
// Nhóm lỗi theo nguồn trong nhật ký sự kiện Windows; tên nguồn/mã sự kiện là chuẩn của Microsoft.
const ERRCAT = { crash: ['Màn hình xanh, tắt đột ngột', 'crit'], disk: ['Ổ cứng', 'crit'], hw: ['Phần cứng', 'crit'], app: ['Ứng dụng bị đóng, treo', 'warn'], drv: ['Driver', 'warn'], update: ['Cập nhật Windows', 'warn'], svc: ['Dịch vụ Windows', 'warn'], mam: ['Cảnh báo MAM', 'info'], other: ['Lỗi khác', 'off'] };
function errCat(e) {
  const s = e.src || '', id = e.eid;
  if (e.mam) return 'mam';
  if ((/Kernel-Power$/.test(s) && id === 41) || (s === 'EventLog' && id === 6008) || /BugCheck|WER-SystemErrorReporting/.test(s)) return 'crash';
  if (/WHEA-Logger/.test(s)) return 'hw';
  if (/^(disk|Ntfs|volmgr|storahci|stornvme|iaStorA?V?|Microsoft-Windows-Ntfs|Microsoft-Windows-StorPort)$/i.test(s)) return 'disk';
  if (s === 'Application Error' || s === 'Application Hang') return 'app';
  if (/WindowsUpdateClient/.test(s)) return 'update';
  if (s === 'Service Control Manager') return 'svc';
  if (/^Display$|nvlddmkm|amdkmdag|igfx|Kernel-PnP/i.test(s)) return 'drv';
  return 'other';
}
function errTitle(e, c) {
  if (c === 'mam') return e.msg;
  if (c === 'crash') return e.eid === 41 ? 'Máy tắt đột ngột (mất điện, treo hoặc giữ nút nguồn)' : e.eid === 6008 ? 'Lần tắt máy trước không đúng cách' : 'Màn hình xanh (BSOD)';
  return { disk: 'Ổ cứng báo lỗi đọc/ghi', hw: 'Lỗi phần cứng (CPU, RAM, bo mạch)', app: e.src === 'Application Hang' ? 'Ứng dụng bị treo (Not Responding)' : 'Ứng dụng bị đóng đột ngột', drv: 'Driver bị lỗi', update: 'Cài bản cập nhật Windows thất bại', svc: 'Dịch vụ Windows bị lỗi hoặc dừng đột ngột' }[c] || e.src;
}
async function errTab(m) {
  const r = await api('GET', '/api/machines/' + m.id + '/events');
  const all = r.events.map(e => ({ ...e })).concat(r.alerts.map(a => ({ at: a.at, mam: 1, src: 'MAM', eid: '', lvl: a.sev === 'crit' ? 1 : 2, msg: a.msg })));
  all.forEach(e => { e.c = errCat(e); });
  all.sort((a, b) => b.at - a.at);
  // gom lỗi giống nhau trong cùng một ngày thành một dòng
  const rows = [], idx = {};
  for (const e of all) {
    const d = new Date(e.at * 1000), k = d.toDateString() + '|' + e.src + '|' + e.eid + '|' + e.msg;
    if (idx[k]) { idx[k].n++; idx[k].first = e.at; continue; }
    rows.push(idx[k] = { ...e, n: 1, first: e.at });
  }
  const cnt = c => all.filter(e => e.c === c).length;
  const wrap = el('div', { style: 'display:flex;flex-direction:column;gap:12px' });
  if (!/win/i.test(r.os || m.os || '')) wrap.append(infoNote('Nhật ký lỗi chỉ đọc được trên máy Windows. Máy này chỉ có cảnh báo của MAM.'));
  wrap.append(el('div', { class: 'kpis up-kpis' },
    kpi('Tổng số lỗi', 'var(--accent)', all.length, t('trong {0} ngày qua', r.days)),
    kpi('Màn hình xanh, tắt đột ngột', 'var(--crit)', cnt('crash'), 'Kernel-Power 41, BugCheck, EventLog 6008'),
    kpi('Ổ cứng và phần cứng', 'var(--warn)', cnt('disk') + cnt('hw'), 'Nên sao lưu dữ liệu nếu tăng dần'),
    kpi('Ứng dụng bị đóng, treo', 'var(--muted)', cnt('app'), 'Application Error, Application Hang')));
  // số lỗi theo tuần, 26 tuần gần nhất
  const w0 = upMon(upToday()), weeks = Array.from({ length: 26 }, (_, i) => ({ d: upAdd(w0, -7 * (25 - i)), v: 0 }));
  for (const e of all) { const k = Math.floor((e.at * 1000 - weeks[0].d.getTime()) / (7 * 864e5)); if (k >= 0 && k < 26) weeks[k].v++; }
  wrap.append(card('Số lỗi theo tuần', '26 tuần gần nhất', upBars(weeks.map((w, i) => ({ lb: i % 2 ? '' : upDD(w.d), v: w.v })), true)));
  const sel = select([['all', t('Tất cả') + ' (' + all.length + ')'], ...Object.keys(ERRCAT).filter(c => cnt(c)).map(c => [c, t(ERRCAT[c][0]) + ' (' + cnt(c) + ')'])], 'all', () => draw());
  const q = el('input', { class: 'input', id: 'errq', placeholder: 'Tìm theo nội dung, nguồn, mã lỗi…', style: 'max-width:300px' });
  const box = el('div', {});
  function draw() {
    const c = sel.value, k = q.value.trim().toLowerCase();
    const l = rows.filter(e => (c === 'all' || e.c === c) && (!k || (e.msg + ' ' + e.src + ' ' + e.eid + ' ' + t(errTitle(e, e.c))).toLowerCase().includes(k)));
    box.replaceChildren(table(['Thời gian', 'Loại', 'Lỗi', 'Nguồn', { t: 'Số lần', cls: 'r' }], l.slice(0, 500).map(e => el('tr', { class: 'nohover' },
      el('td', { class: 'mono', style: 'white-space:nowrap' }, fmtDT(e.at * 1000)),
      el('td', { style: 'white-space:nowrap' }, pill(ERRCAT[e.c][1], ERRCAT[e.c][0])),
      el('td', { style: 'min-width:260px' }, el('div', { style: 'font-weight:600' }, errTitle(e, e.c)), e.mam ? null : el('div', { style: 'font-size:12px;color:var(--faint);margin-top:2px;white-space:normal' }, e.msg)),
      el('td', { style: 'font-size:12.5px;color:var(--muted)' }, e.mam ? 'MAM' : e.src + ' · ' + e.eid),
      el('td', { class: 'r mono' }, e.n > 1 ? '×' + e.n : '1'))), all.length ? 'Không có lỗi nào khớp' : 'Không ghi nhận lỗi nào trong 180 ngày qua'));
    if (l.length > 500) box.append(el('div', { style: 'margin-top:8px;font-size:12px;color:var(--faint)' }, t('Đang hiện 500 dòng mới nhất trong {0} dòng. Dùng ô tìm kiếm để lọc.', l.length)));
  }
  draw(); q.oninput = draw;
  wrap.append(card('Danh sách lỗi', 'lỗi giống nhau trong cùng ngày được gộp một dòng', el('div', {}, el('div', { class: 'filters', style: 'margin-bottom:10px' }, sel, q), box)),
    el('div', { style: 'font-size:12px;color:var(--faint)' }, 'Đọc từ nhật ký sự kiện Windows (System, Application) ở mức Lỗi và Nghiêm trọng, cập nhật khoảng 10 phút một lần, giữ 180 ngày. Cần agent 1.1.48 trở lên; lần đầu agent lấy lại lỗi cũ còn trong nhật ký Windows.'));
  return wrap;
}
// Trang "Thời gian hoạt động": mọi máy trong phạm vi
VIEWS.uptime = async v => {
  if (!UP.group) UP.group = 'all';
  const ds = upDays(), last = ds[ds.length - 1] > upToday() ? upToday() : ds[ds.length - 1];
  const [r, gl] = await Promise.all([upFetch(UP.group !== 'all' ? '&group=' + encodeURIComponent(UP.group) : ''), api('GET', '/api/groups')]);
  groups = gl.groups.map(x => x.name);
  const xq = '?from=' + upISO(ds[0]) + '&to=' + upISO(last) + (UP.group !== 'all' ? '&group=' + encodeURIComponent(UP.group) : '');
  v.append(upBar(select([['all', 'Tất cả nhóm'], ['__none', 'Chưa phân nhóm'], ...groups.map(x => [x, x])], UP.group, x => { UP.group = x; render(); }),
    el('a', { class: 'btn', href: '/api/export/uptime.xlsx' + xq }, 'Xuất Excel'), el('a', { class: 'btn', href: '/api/export/uptime.csv' + xq }, 'Xuất CSV')));
  const one = UP.per === 'day';
  const rows = r.machines.map(m => ({ m, st: upStats(m.s, r.nowMs) })).sort((a, b) => b.st.tot - a.st.tot).map(({ m, st }) => {
    const ss = one ? st.ds[0].ss : [], lt = ss[ss.length - 1];
    return el('tr', { onclick: () => { route.tab = 'up'; route.view = 'detail'; route.id = m.id; renderNav(); render(); scrollTo(0, 0); } },
      el('td', {}, el('div', { style: 'font-weight:600' }, m.name), el('div', { style: 'font-size:11.5px;color:var(--faint)' }, m.group || t('Chưa phân nhóm'))),
      one ? [el('td', { style: 'width:42%;min-width:220px' }, upStrip(ss)), el('td', { class: 'mono' }, ss.length ? upClock(ss[0][0]) : '—'), el('td', {}, !lt ? '—' : lt[2] ? pill('ok', 'Đang bật') : el('span', { class: 'mono' }, upClock(lt[1]))), el('td', { class: 'r mono' }, ss.length)]
        : [el('td', { class: 'r mono' }, st.used.length), el('td', { class: 'r mono' }, st.used.length ? upDur(st.tot / st.used.length) : '—'), el('td', { class: 'mono' }, upAvg(st.firsts)), el('td', { class: 'mono' }, upAvg(st.lasts))],
      el('td', { class: 'r mono', style: 'font-weight:600' }, upDur(st.tot)));
  });
  const heads = one ? ['Máy', 'Các khoảng giờ bật (0h – 24h)', 'Bật lúc', 'Tắt lúc', { t: 'Số lần bật', cls: 'r' }, { t: 'Tổng giờ bật', cls: 'r' }]
    : ['Máy', { t: 'Số ngày bật', cls: 'r' }, { t: 'Trung bình mỗi ngày', cls: 'r' }, 'Bật trung bình lúc', 'Tắt trung bình lúc', { t: 'Tổng giờ bật', cls: 'r' }];
  v.append(el('div', { style: 'margin-top:12px' }, card('Thời gian hoạt động của các máy', 'bấm một máy để xem chi tiết', table(heads, rows, 'Không có máy nào'))));
};
VIEWS.report = async v => {
  const R = VIEWS.report, iso = d => { const z = new Date(d.getTime() - d.getTimezoneOffset() * 6e4); return z.toISOString().slice(0, 10); };
  if (!R.to) { R.to = iso(new Date()); R.from = iso(new Date(Date.now() - 6 * 864e5)); }
  if (!R.group) R.group = 'all';
  const [r, gl] = await Promise.all([api('GET', '/api/report?from=' + R.from + '&to=' + R.to + (R.group !== 'all' ? '&group=' + encodeURIComponent(R.group) : '')), api('GET', '/api/groups')]), days = r.days;
  groups = gl.groups.map(x => x.name);
  const gq = grpQ(R.group);
  const preset = n => { R.to = iso(new Date()); R.from = iso(new Date(Date.now() - (n - 1) * 864e5)); render(); };
  v.append(el('div', { class: 'filters' }, el('div', { class: 'seg' }, [[7, '7 ngày'], [30, '30 ngày'], [90, '90 ngày']].map(([k, l]) => el('button', { class: days === k && R.to === iso(new Date()) ? 'on' : '', onclick: () => preset(k) }, l))), rangePicker(R.from, R.to, (f, tt) => { R.from = f; R.to = tt; render(); }, { max: iso(new Date()), clearable: false }), select([['all', 'Tất cả nhóm'], ['__none', 'Chưa phân nhóm'], ...groups.map(x => [x, x])], R.group, x => { R.group = x; render(); }), el('div', { style: 'flex:1' }),
    el('button', { class: 'btn primary', onclick: () => printReport(r, days, R.group).catch(fail) }, 'Xuất PDF'),
    el('a', { class: 'btn', href: '/api/export/all.xlsx' + gq }, 'Xuất Excel')));
  v.append(el('div', { class: 'kpis', style: 'margin-top:12px' },
    kpi('Tổng máy', 'var(--accent)', r.machines, t('{0} đang online', r.online)),
    kpi('Cảnh báo trong kỳ', 'var(--crit)', r.alerts, t('{0} đang mở hiện tại', r.alertsOpen)),
    kpi('Đóng cảnh báo trung bình', 'var(--good)', r.avgCloseMin ? t('{0} phút', Math.round(r.avgCloseMin)) : '—', 'từ lúc mở đến lúc đóng'),
    kpi('Yêu cầu hỗ trợ', 'var(--warn)', r.requests, t('{0} mới · {1} đã xong', r.requestsNew, r.requestsDone))));
  const kindLabel = { temp: 'Nhiệt độ CPU', cpu: 'CPU cao liên tục', disk: 'Ổ đĩa sắp đầy', offline: 'Mất kết nối', virus: 'Phát hiện virus', suspect: 'Nghi ngờ mã độc', patch: 'Thiếu bản vá Windows' }, kindCol = { temp: 'var(--warn)', cpu: 'var(--accent)', disk: 'var(--crit)', offline: 'var(--faint)', virus: 'var(--crit)', suspect: 'var(--warn)', patch: 'var(--warn)' };
  const hbar = (label, n, max, color) => el('div', { class: 'hrow' }, el('div', { class: 't' }, el('b', {}, label), el('span', { style: 'color:var(--muted)' }, n)), el('div', { class: 'bar' }, el('i', { style: 'width:' + (max ? n / max * 100 : 0) + '%;background:' + color })));
  const byK = Object.entries(r.byKind || {}), maxK = Math.max(0, ...byK.map(x => x[1]));
  const byD = r.byDay || [], maxD = Math.max(1, ...byD.map(x => x.n));
  const chart = el('div', {}, el('div', { class: 'bcols' }, byD.map(d => el('div', { class: 'c' }, el('b', {}, d.n), el('i', { style: 'height:' + Math.round(d.n / maxD * 78) + '%;background:' + (d.n === maxD && d.n > 1 ? 'var(--crit)' : d.n ? 'var(--accent)' : 'var(--surface-3)') })))), el('div', { class: 'collabels' }, byD.map(d => el('span', {}, d.label))));
  v.append(el('div', { class: 'grid2', style: 'margin-top:14px' },
    card(days > 14 ? 'Cảnh báo theo tuần' : 'Cảnh báo theo ngày', null, chart),
    card('Theo loại cảnh báo', null, byK.length ? byK.map(([k, c]) => hbar(kindLabel[k] || k, c, maxK, kindCol[k] || 'var(--accent)')) : el('div', { class: 'empty' }, 'Không có'))));
  const top = r.top || [], maxT = Math.max(1, ...top.map(x => x.count));
  const byT = Object.entries(r.byType || {});
  v.append(el('div', { class: 'grid2', style: 'margin-top:14px' },
    card('Máy phát sinh nhiều cảnh báo nhất', null, top.length ? top.map(x => hbar(x.name, x.count, maxT, 'var(--accent)')) : el('div', { class: 'empty' }, 'Không có')),
    card('Yêu cầu hỗ trợ theo loại thiết bị', null, byT.length ? byT.map(([k, c]) => el('div', { style: 'display:flex;align-items:center;gap:10px;padding:9px 0;border-top:1px solid var(--border)' }, el('b', { style: 'flex:1' }, k), pill('ok', t('{0} xong', c.done || 0)), pill((c.open || 0) ? 'warn' : 'off', t('{0} chưa xong', c.open || 0)))) : el('div', { class: 'empty' }, 'Chưa có yêu cầu'))));
  v.append(el('div', { style: 'margin-top:14px' }, card('Xuất từng bảng', R.group === 'all' ? 'CSV mở được bằng Excel · Excel có định dạng màu' : t('Chỉ gồm nhóm: {0}', grpLabel(R.group)), el('div', { style: 'display:flex;gap:8px;flex-wrap:wrap' },
    ['machines', 'alerts', 'requests'].map(k => el('a', { class: 'btn', href: '/api/export/' + k + '.xlsx' + gq }, { machines: 'Danh sách máy (Excel)', alerts: 'Cảnh báo (Excel)', requests: 'Yêu cầu hỗ trợ (Excel)' }[k])),
    ['machines', 'alerts', 'requests'].map(k => el('a', { class: 'btn', href: '/api/export/' + k + '.csv' + gq }, { machines: 'Máy (CSV)', alerts: 'Cảnh báo (CSV)', requests: 'Yêu cầu (CSV)' }[k]))))));
};


/* ---------- Ảnh đại diện ---------- */
const AVCOL = ['#2563EB', '#0891B2', '#059669', '#D97706', '#DC2626', '#7C3AED', '#DB2777', '#4F46E5'];
function avatarEl(kind, id, ver, name, size) {
  const sz = (size || 32) + 'px', st = 'width:' + sz + ';height:' + sz + ';';
  const w = String(name || '?').trim().split(/\s+/), ini = ((w[0][0] || '') + (w.length > 1 ? w[w.length - 1][0] : '')).toUpperCase();
  let h = 0; for (const c of String(name || id)) h = (h * 31 + c.charCodeAt(0)) >>> 0;
  const ph = el('span', { class: 'av', style: st + 'background:' + AVCOL[h % AVCOL.length] + ';font-size:' + Math.round((size || 32) * .38) + 'px' }, ini);
  if (!ver) return ph;
  const img = el('img', { class: 'av', alt: '', style: st, src: '/api/avatar/' + kind + '/' + id + '?v=' + ver });
  img.onerror = () => img.replaceWith(ph);
  return img;
}
// Chọn ảnh, cắt vuông ở giữa và thu về 256px ngay trên máy (ảnh điện thoại vài MB thành vài chục KB trước khi gửi)
function pickImage(cb) {
  const f = el('input', { type: 'file', accept: 'image/*' });
  f.onchange = () => {
    const file = f.files[0]; if (!file) return;
    const im = new Image(), u = URL.createObjectURL(file);
    im.onload = () => {
      const c = document.createElement('canvas'), g = c.getContext('2d'), W = im.naturalWidth, H = im.naturalHeight; c.width = c.height = 256;
      // giữ trọn cả bức ảnh (không cắt): ảnh nằm vừa khung, phần thừa phủ bằng chính ảnh đó làm mờ
      const k = Math.max(256 / W, 256 / H), f = Math.min(256 / W, 256 / H);
      try { g.filter = 'blur(14px)'; } catch (e) { }
      g.drawImage(im, (256 - W * k) / 2, (256 - H * k) / 2, W * k, H * k); g.filter = 'none';
      g.drawImage(im, (256 - W * f) / 2, (256 - H * f) / 2, W * f, H * f);
      URL.revokeObjectURL(u); c.toBlob(b => cb(b), 'image/jpeg', .85);
    };
    im.onerror = () => { URL.revokeObjectURL(u); toast('Không đọc được ảnh này', 'warn'); };
    im.src = u;
  };
  f.click();
}
async function putAvatar(blob) {
  const r = await fetch('/api/avatar/u/' + me.id, { method: 'POST', headers: { 'X-MAM': '1' }, body: blob });
  const d = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(d.error || 'Lỗi');
  return d.av;
}
function avatarCard() {
  const box = el('div', {});
  const draw = async () => {
    box.innerHTML = '';
    let pol = { machines: true }; try { pol = await api('GET', '/api/avatar-policy'); } catch (e) { }
    const row = el('div', { style: 'display:flex;align-items:center;gap:14px;flex-wrap:wrap' }, avatarEl('u', me.id, me.av, me.name, 64),
      el('div', { style: 'display:flex;gap:8px;flex-wrap:wrap' },
        el('button', { class: 'btn primary', onclick: () => pickImage(async b => { try { me.av = await putAvatar(b); setWho(); toast('Đã đổi ảnh đại diện'); render(); } catch (e) { fail(e); } }) }, 'Chọn ảnh'),
        me.av ? el('button', { class: 'btn', onclick: async () => { try { await api('DELETE', '/api/avatar/u/' + me.id); me.av = 0; setWho(); render(); } catch (e) { fail(e); } } }, 'Xóa ảnh') : null));
    box.append(row);
    if (isSuper()) { const cb = el('input', { type: 'checkbox', checked: pol.machines, onchange: async () => { try { await api('PUT', '/api/avatar-policy', { machines: cb.checked }); toast(cb.checked ? 'Máy được tự đổi ảnh' : 'Máy không được tự đổi ảnh nữa'); } catch (e) { fail(e); } } });
      box.append(el('label', { style: 'display:flex;align-items:center;gap:8px;margin-top:14px;font-size:13px' }, cb, t('Cho phép máy tự đổi ảnh đại diện (ngay trong cửa sổ MAM trên máy)'))); }
  };
  draw();
  return card('Ảnh đại diện', 'hiện cạnh tin nhắn chat', box);
}
/* ---------- Tài khoản ---------- */
function passModal() {
  const cur = input('', 'password'), np = input('', 'password'), cp = input('', 'password');
  const f1 = field('Mật khẩu hiện tại', cur), f2 = field('Mật khẩu mới', np, 'Ít nhất 8 ký tự, có chữ và số'), f3 = field('Nhập lại mật khẩu mới', cp);
  modal('Đổi mật khẩu của tôi', el('div', { style: 'display:flex;flex-direction:column;gap:12px' }, f1, f2, f3), [{ label: 'Hủy', on: c => c() }, { label: 'Lưu', cls: 'primary', on: async c => {
    const a = setErr(f2, np.value.length < 8 ? 'Ít nhất 8 ký tự' : !/[A-Za-z]/.test(np.value) || !/\d/.test(np.value) ? 'Cần có cả chữ và số' : np.value === cur.value ? 'Phải khác mật khẩu cũ' : ''), b = setErr(f3, cp.value === np.value ? '' : 'Hai mật khẩu không khớp');
    if (!(a && b)) return;
    try { await api('POST', '/api/me/password', { current: cur.value, new: np.value }); c(); toast('Đã đổi mật khẩu'); } catch (e) { setErr(f1, e.message); } } }]);
}
function tfaModal() {
  if (me.totp) {
    const pw = input('', 'password'), cd = input('6 số trong ứng dụng xác thực'), f1 = field('Mật khẩu hiện tại', pw), f2 = field('Mã xác thực hiện tại', cd);
    modal('Tắt xác thực hai bước', el('div', { style: 'display:flex;flex-direction:column;gap:12px' }, f1, f2), [{ label: 'Hủy', on: c => c() }, { label: 'Tắt', cls: 'danger', on: async c => {
      try { await api('POST', '/api/me/2fa/disable', { password: pw.value, code: cd.value }); me.totp = false; c(); toast('Đã tắt xác thực hai bước'); render(); } catch (e) { setErr(f2, e.message); } } }]);
    return;
  }
  api('POST', '/api/me/2fa/start').then(r => {
    const cd = input('6 số trong ứng dụng xác thực'), f = field('Mã xác nhận', cd);
    modal('Bật xác thực hai bước', el('div', { style: 'font-size:13px;line-height:1.6;display:flex;flex-direction:column;gap:12px' },
      el('div', {}, 'Mở Google Authenticator (hoặc Microsoft Authenticator, Authy), chọn thêm tài khoản bằng khóa nhập tay, rồi nhập khóa dưới đây. Loại khóa: theo thời gian.'),
      el('div', { class: 'mono', style: 'font-size:17px;font-weight:600;letter-spacing:1px;user-select:all;word-break:break-all' }, r.secret), f),
      [{ label: 'Hủy', on: c => c() }, { label: 'Bật', cls: 'primary', on: async c => {
        try { await api('POST', '/api/me/2fa/enable', { code: cd.value }); me.totp = true; c(); toast('Đã bật xác thực hai bước'); render(); } catch (e) { setErr(f, e.message); } } }]);
  }).catch(fail);
}
const tfaCard = () => card('Xác thực hai bước', me.totp ? 'đang bật' : 'đang tắt', infoNote('Sau mật khẩu, đăng nhập còn cần mã 6 số đổi mỗi 30 giây từ điện thoại. Nên bật cho Super admin, nhất là khi server mở ra ngoài mạng nội bộ.'), el('button', { class: me.totp ? 'btn' : 'btn primary', style: 'padding:6px 12px;font-size:12.5px', onclick: tfaModal }, me.totp ? 'Tắt' : 'Bật'));
const passCard = () => card('Mật khẩu của tôi', t('đang đăng nhập: {0} ({1})', me.name, me.user), infoNote('Đổi mật khẩu của chính bạn ở đây.'), el('button', { class: 'btn primary', style: 'padding:6px 12px;font-size:12.5px', onclick: passModal }, 'Đổi mật khẩu'));
function accModal(a) {
  const nm = input('Họ tên', 'text', a && a.name), un = input('vd phuc.le', 'text', a && a.user), pw = input('Ít nhất 8 ký tự, có chữ và số', 'password');
  if (a) un.disabled = true;
  const rl = select([['tech', 'Kỹ thuật viên'], ['view', 'Chỉ xem'], ['super', 'Super admin']], a ? a.role : 'tech');
  const chks = groups.map(g => el('label', { class: 'chk' }, el('input', { type: 'checkbox', class: 'sel', value: g, checked: a && (a.scope || []).includes(g) }), g));
  const fn = field('Họ tên', nm), fu = field('Tài khoản đăng nhập', un, a ? null : 'Chữ thường, số, dấu . _'), fp = a ? null : field('Mật khẩu ban đầu', pw, 'Người dùng sẽ bị bắt đổi ngay khi đăng nhập lần đầu'), fr = field('Vai trò', rl), fs = field('Phạm vi nhóm máy (Kỹ thuật viên)', el('div', { style: 'display:flex;flex-direction:column;gap:6px' }, chks.length ? chks : el('span', { style: 'color:var(--faint);font-size:12.5px' }, 'Chưa có nhóm nào')), 'Không tick nhóm nào = tất cả các nhóm');
  const sync = () => { fs.style.display = rl.value === 'tech' ? '' : 'none'; }; rl.addEventListener('change', sync); sync();
  const actions = [{ label: 'Hủy', on: c => c() }];
  if (a) actions.unshift({ label: 'Xóa', cls: 'danger', on: c => { c(); confirmBox('Xóa tài khoản?', t('Xóa {0} khỏi hệ thống. Không hoàn tác được.', a.user), 'Xóa', true, async () => { try { await api('DELETE', '/api/accounts/' + a.id); toast(t('Đã xóa {0}', a.user)); render(); } catch (e) { fail(e); } }); } },
    { label: 'Đặt lại mật khẩu', on: c => { c(); confirmBox('Đặt lại mật khẩu?', t('{0} sẽ nhận một mật khẩu tạm và bị đăng xuất ngay. Họ phải đổi mật khẩu khi đăng nhập lại.', a.user), 'Đặt lại', false, async () => { try { const r = await api('POST', '/api/accounts/' + a.id + '/reset', {}); tempShow(a.user, r.tempPassword); } catch (e) { fail(e); } }); } });
  if (a && a.totp) actions.unshift({ label: 'Gỡ 2FA', on: c => { c(); confirmBox('Gỡ xác thực hai bước?', t('{0} sẽ đăng nhập chỉ bằng mật khẩu cho tới khi tự bật lại. Dùng khi họ mất điện thoại.', a.user), 'Gỡ', false, async () => { try { await api('POST', '/api/accounts/' + a.id + '/reset2fa'); toast('Đã gỡ xác thực hai bước'); render(); } catch (e) { fail(e); } }); } });
  actions.push({ label: 'Lưu', cls: 'primary', on: async c => {
    let ok = true;
    if (!setErr(fn, nm.value.trim().length >= 2 ? '' : 'Nhập họ tên')) ok = false;
    if (!a && !setErr(fu, /^[a-z0-9._]{3,32}$/.test(un.value.trim()) ? '' : 'Tài khoản 3 đến 32 ký tự: chữ thường, số, dấu . _')) ok = false;
    if (!a && !setErr(fp, pw.value.length < 8 ? 'Ít nhất 8 ký tự' : !/[A-Za-z]/.test(pw.value) || !/\d/.test(pw.value) ? 'Cần có cả chữ và số' : '')) ok = false;
    if (!ok) return;
    const scope = rl.value === 'tech' ? chks.map(l => l.firstChild).filter(i => i.checked).map(i => i.value) : [];
    try { if (a) await api('PUT', '/api/accounts/' + a.id, { name: nm.value, role: rl.value, scope }); else await api('POST', '/api/accounts', { name: nm.value, user: un.value.trim(), password: pw.value, role: rl.value, scope }); c(); toast(a ? t('Đã cập nhật {0}', a.user) : t('Đã thêm tài khoản')); render(); } catch (e) { setErr(a ? fn : fu, e.message); } } });
  modal(a ? t('Sửa tài khoản · {0}', a.user) : t('Thêm tài khoản'), el('div', { style: 'display:flex;flex-direction:column;gap:12px' }, fn, fu, fp, fr, fs), actions);
}
function tempShow(user, tp) {
  modal('Mật khẩu tạm', el('div', { style: 'line-height:1.6;font-size:13px' }, t('Mật khẩu tạm của {0} (chỉ hiện một lần):', user), el('div', { class: 'mono', style: 'font-size:20px;font-weight:600;margin:10px 0;letter-spacing:.5px;user-select:all' }, tp), el('div', { style: 'color:var(--muted);font-size:12.5px' }, 'Đưa cho họ qua kênh an toàn. Đăng nhập xong hệ thống bắt đặt mật khẩu mới.')),
    [{ label: 'Sao chép', on: () => { try { navigator.clipboard.writeText(tp); toast('Đã sao chép'); } catch (e) { toast('Hãy chọn và chép thủ công', 'warn'); } } }, { label: 'Xong', cls: 'primary', on: c => c() }]);
}
VIEWS.accounts = async v => {
  if (!isSuper()) { v.append(infoNote('Chỉ Super admin quản lý tài khoản và quyền. Bạn đổi được mật khẩu của chính mình ở đây.'), el('div', { style: 'margin-top:14px' }, avatarCard()), el('div', { style: 'margin-top:14px' }, tfaCard()), el('div', { style: 'margin-top:14px' }, passCard())); return; }
  const list = await api('GET', '/api/accounts');
  v.append(passCard(), el('div', { style: 'margin-top:14px' }, avatarCard()), el('div', { style: 'margin-top:14px' }, tfaCard()), el('div', { style: 'margin-top:14px' }, card('Tài khoản admin', 'bấm vào dòng để sửa quyền', el('div', { style: 'margin:0 -16px -8px' }, table(['Tên', { t: 'Tên đăng nhập', cls: 'hide-s' }, 'Vai trò', { t: 'Phạm vi', cls: 'hide-s' }, { t: 'Đăng nhập cuối', cls: 'hide-s' }], list.map(a => el('tr', { onclick: () => accModal(a) }, el('td', { style: 'font-weight:600' }, a.name, a.mustChange ? el('div', { style: 'font-size:11.5px;color:var(--warn)' }, 'Chờ đổi mật khẩu') : ''), el('td', { class: 'hide-s mono' }, a.user), el('td', {}, el('span', { class: 'rolechip ' + a.role }, ROLE[a.role])), el('td', { class: 'hide-s' }, a.role === 'super' ? 'Toàn bộ' : (a.scope || []).length ? a.scope.join(', ') : 'Tất cả nhóm'), el('td', { class: 'hide-s' }, ago(a.lastLogin)))))), el('button', { class: 'btn primary', style: 'padding:6px 12px;font-size:12.5px', onclick: () => accModal(null) }, '+ Thêm tài khoản'))));
  v.append(el('div', { style: 'margin-top:12px' }, infoNote('Super admin: toàn quyền. Kỹ thuật viên: xem máy, xử lý cảnh báo, ra lệnh cho máy trong nhóm được giao; không quản lý tài khoản, nhóm, cài đặt. Chỉ xem: chỉ theo dõi. Server kiểm tra quyền ở từng thao tác.')));
};

/* ---------- Nhật ký ---------- */
const UF = { q: '', who: '' };
VIEWS.audit = async v => {
  const qs = '?q=' + encodeURIComponent(UF.q) + '&who=' + encodeURIComponent(UF.who);
  const [list, accs] = await Promise.all([api('GET', '/api/audit' + qs), isSuper() ? api('GET', '/api/accounts') : Promise.resolve([])]);
  v.append(infoNote(isSuper() ? 'Mọi thao tác trong Console đều được ghi: ai làm, lúc nào, từ IP nào, lên đối tượng nào. Nhật ký chỉ ghi thêm, không có nút sửa hay xóa, kể cả với Super admin. Hiện tối đa 500 dòng mới nhất.' : 'Bạn đang xem nhật ký của riêng mình. Chỉ Super admin xem được thao tác của thành viên khác.'));
  v.append(el('div', { class: 'filters', style: 'margin-top:12px' }, el('input', { class: 'input grow', id: 'asearch', type: 'search', placeholder: 'Tìm theo người, nội dung, máy, IP…', value: UF.q, onchange: e => { UF.q = e.target.value; render(); } }),
    isSuper() ? select([['', 'Mọi thành viên'], ...accs.map(a => [a.user, a.name]), ['system', 'Hệ thống']], UF.who, x => { UF.who = x; render(); }) : null,
    el('span', { style: 'color:var(--muted);font-size:12.5px' }, t('{0} dòng', list.length))));
  v.append(el('div', { class: 'card', style: 'margin-top:12px' }, el('div', { class: 'card-b', style: 'padding:0 16px 8px' }, table(['Thời gian', 'Thành viên', { t: 'IP', cls: 'hide-s' }, 'Loại', 'Nội dung'], list.map(e => el('tr', { class: 'nohover' }, el('td', { class: 'mono', style: 'white-space:nowrap' }, fmtDT(e.t)), el('td', {}, e.name, el('div', { style: 'font-size:11.5px;color:var(--faint)' }, e.user)), el('td', { class: 'hide-s mono' }, e.ip), el('td', {}, e.kind, e.sens ? el('div', {}, pill('warn', 'Nhạy cảm')) : ''), el('td', {}, e.msg))), 'Chưa có thao tác nào'))));
};

/* ---------- Cài đặt ---------- */
VIEWS.settings = async v => {
  settings = await api('GET', '/api/settings');
  const f = k => { const i = input('', 'number', settings[k]); if (!isSuper()) i.disabled = true; return i; };
  const ft = f('temp'), c = f('cpu'), d = f('disk'), o = f('offline');
  const key = input('Để trống = máy nào cài agent cũng tự đăng ký', 'text', settings.enrollKey || ''); if (!isSuper()) key.style.display = 'none';
  const fe = field('Nhiệt độ CPU cảnh báo (°C)', ft), fc = field('CPU cảnh báo (%) khi cao liên tục khoảng 30 giây', c), fd = field('Ổ cứng cảnh báo khi trống còn (%)', d), fo = field('Mất kết nối cảnh báo sau (phút)', o);
  v.append(card('Ngưỡng cảnh báo', isSuper() ? null : 'chỉ Super admin được sửa', el('div', { class: 'row2' }, fe, fc, fd, fo), isSuper() ? el('button', { class: 'btn primary', style: 'padding:6px 12px;font-size:12.5px', onclick: async () => {
    try { await api('PUT', '/api/settings', { temp: +ft.value, cpu: +c.value, disk: +d.value, offline: +o.value, enrollKey: key.value }); toast('Đã lưu ngưỡng cảnh báo'); settings = await api('GET', '/api/settings'); } catch (e) { fail(e); } } }, 'Lưu') : null));
  if (isSuper()) {
    const rp = (await api('GET', '/api/remote-password')).set, pwi = input(rp ? 'Nhập mật khẩu mới để đổi' : 'Ít nhất 8 ký tự', 'password');
    const sv = async v => { try { await api('PUT', '/api/remote-password', { password: v }); toast(v ? 'Đã lưu mật khẩu điều khiển từ xa' : 'Đã tắt mật khẩu điều khiển từ xa'); render(); } catch (e) { fail(e); } };
    v.append(card('Mật khẩu điều khiển từ xa', rp ? 'đang bật' : 'đang tắt: máy phải bấm Đồng ý', field('Mật khẩu', pwi, 'Admin nhập đúng mật khẩu này khi mở điều khiển màn hình là vào thẳng, không cần người ngồi máy đồng ý. Máy vẫn hiện thanh báo và nút Dừng. Để tắt thì bấm Tắt.'),
      el('div', { style: 'display:flex;gap:8px;margin-top:10px' }, el('button', { class: 'btn primary', style: 'padding:6px 12px;font-size:12.5px', onclick: () => pwi.value && sv(pwi.value) }, 'Lưu'), rp ? el('button', { class: 'btn', style: 'padding:6px 12px;font-size:12.5px', onclick: () => sv('') }, 'Tắt') : null)));
    // Mật khẩu gỡ agent: người ngồi máy nhập để gỡ, khỏi lộ tài khoản quản trị
    const up = await api('GET', '/api/uninstall-password').catch(() => ({ set: false }));
    const u1 = input('Ít nhất 8 ký tự', 'password'), u2 = input('Nhập lại mật khẩu gỡ', 'password');
    const usv = async () => {
      if (u1.value.length < 8) { toast(t('Mật khẩu gỡ ít nhất 8 ký tự'), 'warn'); return; }
      if (u1.value !== u2.value) { toast(t('Hai ô mật khẩu gỡ không khớp'), 'warn'); return; }
      const val = u1.value;
      try { await api('PUT', '/api/uninstall-password', { password: val }); } catch (e) { fail(e); return; }
      // server chỉ giữ bản băm: hiện mật khẩu một lần để admin ghi lại
      modal('Đã lưu mật khẩu gỡ', el('div', { style: 'display:flex;flex-direction:column;gap:10px' },
        el('div', { style: 'font-size:13px;color:var(--muted)' }, 'Ghi lại mật khẩu này ngay. Sau khi đóng, không ai xem lại được.'),
        el('div', { class: 'mono', style: 'font-size:20px;font-weight:700;letter-spacing:1px;padding:12px 14px;border-radius:10px;background:var(--surface-2);user-select:all' }, val)),
        [{ label: 'Đã ghi lại', cls: 'primary', on: c => { c(); render(); } }]);
    };
    const rnd = () => { const c = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789', b = new Uint32Array(12); crypto.getRandomValues(b); const s = Array.from(b, x => c[x % c.length]).join(''); const v = s.slice(0, 4) + '-' + s.slice(4, 8) + '-' + s.slice(8); u1.value = u2.value = v; u1.type = u2.type = 'text'; };
    const upill = el('span', { style: 'display:inline-flex;align-items:center;gap:6px;padding:3px 10px;border-radius:999px;font-size:12px;font-weight:700;' + (up.set ? 'background:rgba(22,163,74,.14);color:var(--good)' : 'background:var(--surface-3);color:var(--muted)') }, up.set ? t('Đã đặt') : t('Chưa đặt'));
    v.append(el('div', { style: 'margin-top:14px' }, card('Mật khẩu gỡ agent', 'người ngồi máy nhập để gỡ MAM Agent, không cần tài khoản quản trị', el('div', { style: 'display:flex;flex-direction:column;gap:12px' },
      el('div', { style: 'font-size:13px;color:var(--muted)' }, up.set && up.at ? t('Đổi lần cuối {0} bởi {1}. Server chỉ lưu bản băm, không ai xem lại được mật khẩu cũ.', fmtDT(up.at), up.by || '—') : t('Chưa đặt thì không ai gỡ được agent trên máy, chỉ gỡ được bằng nút Gỡ máy trong Console.')),
      el('div', { class: 'row2' }, field('Mật khẩu gỡ mới', u1), field('Nhập lại', u2)),
      el('div', { style: 'font-size:12px;color:var(--muted)' }, 'Đổi xong, mật khẩu cũ hết hiệu lực ngay trên mọi máy. Đưa mật khẩu này cho người cần gỡ, đổi lại sau khi gỡ xong.'),
      el('div', { style: 'display:flex;gap:8px;flex-wrap:wrap' },
        el('button', { class: 'btn primary', style: 'padding:6px 12px;font-size:12.5px', onclick: usv }, 'Lưu mật khẩu gỡ'),
        el('button', { class: 'btn', style: 'padding:6px 12px;font-size:12.5px', onclick: rnd }, 'Tạo ngẫu nhiên'),
        up.set ? el('button', { class: 'btn danger', style: 'padding:6px 12px;font-size:12.5px;margin-left:auto', onclick: () => confirmBox('Xóa mật khẩu gỡ?', 'Sau khi xóa, không ai gỡ được agent trên máy.', 'Xóa', true, async () => { try { await api('DELETE', '/api/uninstall-password'); toast('Đã xóa mật khẩu gỡ'); render(); } catch (e) { fail(e); } }) }, 'Xóa mật khẩu gỡ') : null)), upill)));
    // Cập nhật agent: server giữ bản agent mới, máy tự tải khi báo cáo (có thể tắt)
    const au = await api('GET', '/api/agent-update').catch(() => null);
    if (au) {
      const auCb = el('input', { type: 'checkbox', checked: au.auto, onchange: async e => { try { await api('PUT', '/api/agent-update', { auto: e.target.checked }); toast(e.target.checked ? 'Đã bật tự cập nhật agent' : 'Đã tắt tự cập nhật agent'); } catch (er) { e.target.checked = !e.target.checked; fail(er); } } });
      v.append(el('div', { style: 'margin-top:14px' }, card('Cập nhật agent', 'máy tự tải bản mới từ server, không cần đến từng máy', el('div', { style: 'display:flex;flex-direction:column;gap:10px;font-size:13px' },
        el('label', { style: 'display:flex;gap:8px;align-items:center' }, auCb, 'Tự cập nhật agent khi server có bản mới'),
        el('div', { style: 'color:var(--muted)' }, au.agentVer ? t('Bản agent trên server: {0} · Máy đang dùng bản cũ: {1}', au.agentVer, au.outdated) : 'Server chưa có file mam-agent.exe nên chưa cập nhật được.'),
        infoNote('Cách cập nhật: chạy bộ cài MAM-Server-Setup.exe bản mới trên máy server (dữ liệu giữ nguyên). Mỗi máy sẽ tự tải bản agent mới, kiểm tra SHA-256 rồi tự thay thế và khởi động lại trong vài phút. Chỉ cập nhật khi số phiên bản mới cao hơn.')))));
    }
    // Dọn dữ liệu cũ: chọn loại + khoảng ngày, xem trước số mục rồi mới xóa (server tự sao lưu trước)
    const PG = VIEWS.settings.pg || (VIEWS.settings.pg = { kinds: new Set(['requests']), from: '', to: '', counts: null });
    const pgKinds = [['requests', 'Yêu cầu hỗ trợ đã xong'], ['alerts', 'Cảnh báo đã đóng'], ['chat', 'Tin nhắn chat với máy'], ['commands', 'Lệnh đã gửi tới máy (xong/lỗi)'], ['audit', 'Nhật ký thao tác (chỉ xóa được mục cũ hơn 30 ngày)']];
    const agoD = n => isoL(new Date(Date.now() - n * 864e5));
    const setPg = (f, tt) => { PG.from = f; PG.to = tt; PG.counts = null; render(); };
    const runPg = async dry => { try { const r = await api('POST', '/api/purge', { kinds: [...PG.kinds], from: PG.from, to: PG.to, dry }); if (dry) PG.counts = r.counts; else { toast(t('Đã xóa {0} mục', Object.values(r.counts).reduce((x, y) => x + y, 0))); PG.counts = null; } render(); } catch (e) { fail(e); } };
    const pgTot = PG.counts ? Object.values(PG.counts).reduce((x, y) => x + y, 0) : 0;
    v.append(card('Dọn dữ liệu', 'xóa bớt dữ liệu cũ theo khoảng ngày', el('div', {},
      el('div', { style: 'font-size:12.5px;color:var(--muted);margin-bottom:8px' }, 'Chỉ xóa mục đã xong/đã đóng; yêu cầu đang xử lý và cảnh báo đang mở luôn được giữ lại.'),
      el('div', { style: 'display:flex;flex-direction:column;gap:6px;margin-bottom:12px' }, ...pgKinds.map(([k, l]) => el('label', { style: 'display:flex;gap:8px;align-items:center;font-size:13px' }, el('input', { type: 'checkbox', checked: PG.kinds.has(k), onchange: e => { e.target.checked ? PG.kinds.add(k) : PG.kinds.delete(k); PG.counts = null; render(); } }), l))),
      el('div', { class: 'frow' }, el('span', { class: 'flab' }, 'Khoảng ngày cần xóa'), ...[['Cũ hơn 30 ngày', 30], ['Cũ hơn 90 ngày', 90], ['Cũ hơn 6 tháng', 180], ['Cũ hơn 1 năm', 365]].map(([l, n]) => el('button', { class: 'chip' + (!PG.from && PG.to === agoD(n) ? ' on' : ''), onclick: () => setPg('', agoD(n)) }, l)), el('span', { class: 'fsep' }), rangePicker(PG.from, PG.to, setPg, { clearable: false })),
      el('div', { style: 'display:flex;gap:8px;align-items:center;flex-wrap:wrap;margin-top:12px' },
        el('button', { class: 'btn', disabled: !PG.to || !PG.kinds.size, onclick: () => runPg(true) }, 'Xem trước'),
        PG.counts ? el('span', { style: 'font-size:13px' }, pgTot ? t('Sẽ xóa {0} mục', pgTot) + ': ' + pgKinds.filter(([k]) => PG.counts[k]).map(([k, l]) => t(l.replace(/ \(.*\)$/, '')) + ' ' + PG.counts[k]).join(', ') : t('Không có mục nào trong khoảng này')) : null,
        PG.counts && pgTot ? el('button', { class: 'btn danger', onclick: () => confirmBox('Xóa vĩnh viễn?', t('Sẽ xóa {0} mục. Hệ thống tự sao lưu một bản ngay trước khi xóa, nên lỡ tay vẫn khôi phục được.', pgTot), 'Xóa', true, () => runPg(false)) }, 'Xóa vĩnh viễn') : null))));
  }
  const origin = location.origin;
  const dl = async (osKey) => {
    try {
      const r = await fetch('/api/agent-download?lang=' + MAMI.lang() + (osKey === 'windows32' ? '&os=windows32' : ''), { credentials: 'same-origin' });
      if (!r.ok) { let m = 'Lỗi ' + r.status; try { m = (await r.json()).error || m; } catch (e) { } throw new Error(m); }
      const a = el('a', { href: URL.createObjectURL(await r.blob()), download: osKey === 'windows32' ? 'MAM-Agent-x86.exe' : 'MAM-Agent.exe' }); document.body.append(a); a.click(); a.remove();
      toast('Đã tải MAM-Agent.exe');
    } catch (e) { fail(e); }
  };
  if (isSuper()) v.append(el('div', { style: 'margin-top:14px' }, card('Cách dễ nhất: tải agent đã cài sẵn', 'Windows 10, 11, Server 2016 trở lên', el('div', { style: 'display:flex;flex-direction:column;gap:10px' },
    el('div', { style: 'font-size:12.5px;color:var(--muted)' }, 'File này đã có sẵn địa chỉ server và mã đăng ký. Chép sang máy client, bấm đúp, chọn Yes ở hộp thoại quyền Administrator. Máy sẽ hiện trên Console sau vài giây. Cài ngầm không hỏi: MAM-Agent.exe /S'),
    el('div', {}, el('button', { class: 'btn primary', style: 'padding:6px 12px;font-size:12.5px', onclick: () => dl() }, 'Tải MAM-Agent.exe'))))));
  if (isSuper()) {
    let have = {}; try { have = await api('GET', '/api/agent-downloads'); } catch (e) { }
    const kq = settings && settings.enrollKey ? ' ' + settings.enrollKey : '';
    const cmd = (os, h) => el('div', { style: 'display:flex;gap:8px;align-items:flex-start;margin-top:6px' }, el('div', { class: 'pre', style: 'flex:1;min-width:0;margin:0;overflow-x:auto' }, h),
      el('button', { class: 'btn', style: 'padding:4px 10px;font-size:12px;flex:none', onclick: () => { try { navigator.clipboard.writeText(h); toast('Đã sao chép'); } catch (e) { toast('Hãy chọn và chép thủ công', 'warn'); } } }, 'Sao chép'));
    const miss = (k, what) => have[k] ? null : el('div', { style: 'font-size:12.5px;color:var(--warn);margin-top:4px' }, t('Server chưa có file {0}. Đặt file này cạnh mam-server rồi tải lại trang.', what));
    const linuxOk = have['linux-amd64'] || have['linux-arm64'], macOk = have['mac-amd64'] || have['mac-arm64'];
    v.append(el('div', { style: 'margin-top:14px' }, card('Tải agent cho hệ điều hành khác', 'Windows 32-bit, Linux, macOS, Android', el('div', { style: 'display:flex;flex-direction:column;gap:14px' },
      el('div', {}, el('b', {}, 'Windows 32-bit (bản cũ)'), el('div', { style: 'margin-top:6px' }, el('button', { class: 'btn', disabled: !have.windows32, onclick: () => dl('windows32') }, 'Tải MAM-Agent-x86.exe')), miss('windows32', 'mam-agent-x86.exe')),
      el('div', {}, el('b', {}, 'Linux (x86-64 và ARM64)'), el('div', { style: 'font-size:12.5px;color:var(--muted);margin-top:4px' }, 'Chạy dòng lệnh này trên máy Linux. Máy tự tải agent từ server rồi cài thành dịch vụ.'), cmd('linux', 'curl -fsSL ' + origin + '/install/linux.sh | sudo sh -s --' + kq), linuxOk ? null : miss('linux-amd64', 'mam-agent-linux-amd64')),
      el('div', {}, el('b', {}, 'macOS (Apple Silicon và Intel)'), el('div', { style: 'font-size:12.5px;color:var(--muted);margin-top:4px' }, 'Chạy dòng lệnh này trong Terminal của máy Mac. Cần cấp quyền Ghi màn hình cho agent nếu muốn xem màn hình từ xa.'), cmd('mac', 'curl -fsSL ' + origin + '/install/mac.sh | sudo sh -s --' + kq), macOk ? null : miss('mac-arm64', 'mam-agent-mac-arm64')),
      el('div', {}, el('b', {}, 'Android'), have.android ? el('div', { style: 'margin-top:6px' }, el('a', { class: 'btn', href: '/dl/MAM-Agent.apk' }, 'Tải MAM-Agent.apk')) : el('div', { style: 'font-size:12.5px;color:var(--muted);margin-top:4px' }, 'Chưa có file APK. App Android nằm ở thư mục agent/android trong gói cài, cần build bằng Android Studio rồi đặt file MAM-Agent.apk cạnh mam-server.'),
        el('div', { style: 'font-size:12.5px;color:var(--muted);margin-top:4px' }, t('App chỉ báo trạng thái máy và nhận tin nhắn, không xem hay điều khiển màn hình được (Android không cho phép). Mở app, nhập {0} rồi bấm Bật quản lý.', origin.replace(/^https?:\/\//, ''))))))));
  }
  v.append(el('div', { style: 'margin-top:14px' }, card('Cài thủ công bằng dòng lệnh', 'chạy bằng quyền Administrator', el('div', { style: 'display:flex;flex-direction:column;gap:10px' },
    el('div', {}, el('b', {}, 'Windows 10, 11, Server 2016 trở lên'), el('div', { class: 'pre', style: 'margin-top:6px' }, 'mam-agent.exe install -server ' + origin)),
    el('div', {}, el('b', {}, 'Windows 7, 8, Server 2008 R2 trở lên (PowerShell)'), el('div', { class: 'pre', style: 'margin-top:6px' }, 'powershell -ExecutionPolicy Bypass -File MAM-Agent.ps1 -Install -Server ' + origin)),
    el('div', {}, el('b', {}, 'Gỡ agent'), el('div', { style: 'font-size:12.5px;color:var(--muted);margin-top:4px' }, 'Chạy mam-agent.exe uninstall (hoặc MAM-Agent.ps1 -Uninstall) trên máy đó. Phải nhập tài khoản và mật khẩu Super admin, người dùng thường không gỡ được.')),
    isSuper() ? el('div', {}, field('Mã đăng ký (tùy chọn)', key, 'Nếu đặt, mọi máy phải có mã này khi cài (thêm -key MÃ vào lệnh cài). Bấm Lưu ở khung trên để áp dụng.')) : null))));
  if (isSuper()) {
    let cur = ''; try { cur = (await api('GET', '/api/data-dir')).dir; } catch (e) { }
    const curEl = el('div', { class: 'pre', style: 'margin-top:6px' }, cur);
    const np = input('Ví dụ D:\\MAMdata (thư mục mới phải trống hoặc chưa có)', 'text', '');
    v.append(el('div', { style: 'margin-top:14px' }, card('Nơi lưu dữ liệu', 'chỉ Super admin', el('div', { style: 'display:flex;flex-direction:column;gap:10px' },
      el('div', {}, el('b', {}, 'Thư mục dữ liệu hiện tại'), curEl),
      field('Chuyển dữ liệu sang thư mục mới', np, 'Server chép tài khoản, máy, nhật ký và bản sao lưu sang thư mục mới rồi dùng ngay chỗ đó, không cần chạy lại. Dữ liệu cũ được giữ nguyên để đại ca tự xóa sau khi kiểm tra.'),
      el('div', {}, el('button', { class: 'btn primary', style: 'padding:6px 12px;font-size:12.5px', onclick: async () => {
        try { const r = await api('POST', '/api/data-dir', { path: np.value }); curEl.textContent = r.dir; np.value = ''; toast('Đã chuyển dữ liệu sang thư mục mới'); } catch (e) { fail(e); } } }, 'Chuyển dữ liệu'))))));
  }
  if (isSuper()) {
    const tg = input('Token bot (nói chuyện với @BotFather để tạo)', 'text', settings.telegram || ''), tgc = input('Chat ID nhận tin (số, hoặc @tenkenh)', 'text', settings.tgChat || '');
    const wh = input('https://...', 'text', settings.webhook || '');
    const sh = input('vd smtp.gmail.com', 'text', settings.smtpHost || ''), sp = input('587', 'text', settings.smtpPort || ''), su = input('tài khoản gửi', 'text', settings.smtpUser || ''), spw = input('mật khẩu / app password', 'password', ''), st2 = input('nơi nhận, cách nhau bằng dấu phẩy', 'text', settings.smtpTo || '');
    // công tắc bật/tắt từng kênh (checked = bật = !xOff)
    const tgOn = el('input', { type: 'checkbox' }), whOn = el('input', { type: 'checkbox' }), smtpOn = el('input', { type: 'checkbox' });
    tgOn.checked = !settings.tgOff; whOn.checked = !settings.whOff; smtpOn.checked = !settings.smtpOff;
    const toggle = (cb, text) => el('label', { style: 'display:inline-flex;align-items:center;gap:6px;font-size:12px;color:var(--muted);cursor:pointer;font-weight:400;margin-left:10px' }, cb, text);
    const head = (title, cb) => el('div', { style: 'display:flex;align-items:center' }, el('b', { style: 'font-size:12.5px' }, title), toggle(cb, 'Bật gửi'));
    v.append(el('div', { style: 'margin-top:14px' }, card('Thông báo cảnh báo ra ngoài', 'gửi khi có cảnh báo mới mở hoặc yêu cầu hỗ trợ gấp', el('div', { style: 'display:flex;flex-direction:column;gap:14px' },
      el('div', {}, head('Telegram', tgOn), el('div', { class: 'row2', style: 'margin-top:8px' }, field('Token bot', tg), field('Chat ID', tgc))),
      el('div', {}, head('Webhook chung (Zalo OA qua trung gian, Slack, n8n...)', whOn), field('', wh, 'Server sẽ POST JSON {"text":"..."} tới địa chỉ này')),
      el('div', {}, head('Email (SMTP)', smtpOn), el('div', { class: 'row2', style: 'margin-top:8px' }, field('Máy chủ SMTP', sh), field('Cổng', sp), field('Tài khoản', su), field('Mật khẩu', spw, 'Để trống = giữ mật khẩu cũ'), field('Gửi tới', st2))),
      el('div', {}, el('button', { class: 'btn primary', style: 'padding:6px 12px;font-size:12.5px', onclick: async () => {
        try { await api('PUT', '/api/settings', Object.assign({}, settings, { telegram: tg.value, tgChat: tgc.value, webhook: wh.value, smtpHost: sh.value, smtpPort: sp.value, smtpUser: su.value, smtpPass: spw.value || undefined, smtpTo: st2.value, tgOff: !tgOn.checked, whOff: !whOn.checked, smtpOff: !smtpOn.checked })); toast('Đã lưu cài đặt thông báo'); settings = await api('GET', '/api/settings'); } catch (e) { fail(e); } } }, 'Lưu thông báo'))))));
  }
  {
    const backups = await api('GET', '/api/backups');
    const list2 = el('div', {});
    const renderBk = () => { list2.innerHTML = ''; list2.append(table(['Thời điểm', 'Dung lượng', ''], backups.map(b => el('tr', { class: 'nohover' }, el('td', { class: 'mono' }, fmtDT(b.time)), el('td', { class: 'mono' }, Math.round(b.size / 1024) + ' KB'), el('td', { style: 'text-align:right' }, isSuper() ? el('a', { class: 'btn', style: 'padding:3px 9px;font-size:12px', href: '/api/backups/' + b.name }, 'Tải') : null))), 'Chưa có bản sao lưu nào')); };
    renderBk();
    v.append(el('div', { style: 'margin-top:14px' }, card('Sao lưu dữ liệu', 'tự động mỗi ngày, giữ 30 bản gần nhất, cùng ổ với dữ liệu', el('div', {}, isSuper() ? el('div', { style: 'margin-bottom:10px' }, el('button', { class: 'btn', style: 'padding:6px 12px;font-size:12.5px', onclick: async () => { try { await api('POST', '/api/backups', {}); toast('Đã sao lưu'); render(); } catch (e) { fail(e); } } }, 'Sao lưu ngay')) : null, list2))));
    if (isSuper()) v.append(el('div', { style: 'margin-top:14px' }, await mirrorCard()));
  }
  v.append(el('div', { style: 'margin-top:14px' }, card('Về MAM', null, el('div', {}, srow('Phiên bản server', (await (await fetch('/api/status')).json()).version), srow('Bản quyền', '© 2026 Nguyễn Tấn Tài (Missing)')))));
};

/* ---------- khởi động ---------- */
$('#theme').addEventListener('click', () => { const n = isDark() ? 'light' : 'dark'; try { localStorage.setItem('mam-theme', n); } catch (e) { } applyTheme(n); });
$('#logout').addEventListener('click', async () => { try { await api('POST', '/api/logout', {}); } catch (e) { } showAuth(); });
$('#menu').addEventListener('click', () => { $('#side').classList.toggle('open'); $('#scrim').classList.toggle('on'); });
$('#scrim').addEventListener('click', () => { $('#side').classList.remove('open'); $('#scrim').classList.remove('on'); });
MAMI.staticPage();
$('#theme').before(MAMI.picker());
try { const th = localStorage.getItem('mam-theme'); if (th) applyTheme(th); } catch (e) { }
(async function boot() {
  const qs = new URLSearchParams(location.search), rid = qs.get('remote');
  try {
    me = await api('GET', '/api/me');
    if (rid) { document.title = 'MAM · ' + (qs.get('n') || ''); screenViewModal({ id: rid, name: qs.get('n') || rid }, true); return; } // cửa sổ điều khiển riêng
    CONTACT = (await api('GET', '/api/contact-label')).label; enter();
  } catch (e) { showAuth(); }
})();
})();

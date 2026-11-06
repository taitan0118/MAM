/* MAM Console — đa ngôn ngữ (vi · en · zh). Khóa là câu tiếng Việt gốc; thiếu bản dịch thì hiện nguyên câu gốc.
 * Bản en/zh do Missing soạn, nên nhờ người bản ngữ rà lại trước khi dùng chính thức. */
(function () {
'use strict';
var EN = @@EN@@;
var ZH = @@ZH@@;
var PAT = @@PAT@@; // [regex, en, zh] cho câu có tham số do server sinh (nhật ký, cảnh báo, lỗi)
var LANGS = [['vi', 'Tiếng Việt'], ['en', 'English'], ['zh', '简体中文']];
var lang = 'vi';
function detect() {
  try { var s = localStorage.getItem('mam-lang'); if (s === 'vi' || s === 'en' || s === 'zh') return s; } catch (e) { }
  var n = (navigator.language || 'vi').toLowerCase();
  return n.indexOf('zh') === 0 ? 'zh' : n.indexOf('en') === 0 ? 'en' : 'vi';
}
lang = detect();
var RX = PAT.map(function (p) { return [new RegExp(p[0]), p[1], p[2]]; });
function fmt(s, a) { return s.replace(/\{(\d+)\}/g, function (m, i) { return a[i] != null ? a[i] : m; }); }
function t(key) {
  if (typeof key !== 'string') return key;
  var a = Array.prototype.slice.call(arguments, 1);
  if (lang === 'vi') return a.length ? fmt(key, a) : key;
  var d = lang === 'zh' ? ZH : EN, hit = d[key];
  if (hit != null) return a.length ? fmt(hit, a) : hit;
  if (!a.length) {
    for (var i = 0; i < RX.length; i++) {
      var m = RX[i][0].exec(key);
      if (m) return (lang === 'zh' ? RX[i][2] : RX[i][1]).replace(/\$(\d)/g, function (x, n) { return t(m[n]); });
    }
  }
  return a.length ? fmt(key, a) : key;
}
function setLang(l) { try { localStorage.setItem('mam-lang', l); } catch (e) { } location.reload(); }
function picker(cls) {
  var s = document.createElement('select');
  s.className = cls || 'langsel'; s.setAttribute('aria-label', t('Ngôn ngữ'));
  LANGS.forEach(function (x) { var o = document.createElement('option'); o.value = x[0]; o.textContent = x[1]; if (x[0] === lang) o.selected = true; s.appendChild(o); });
  s.addEventListener('change', function () { setLang(s.value); });
  return s;
}
// dịch phần HTML tĩnh một lần lúc tải trang
function staticPage() {
  document.documentElement.lang = lang === 'zh' ? 'zh-CN' : lang;
  var w = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT), n, list = [];
  while ((n = w.nextNode())) if (n.nodeValue.trim()) list.push(n);
  list.forEach(function (x) { var v = x.nodeValue.trim(), r = t(v); if (r !== v) x.nodeValue = x.nodeValue.replace(v, r); });
  document.querySelectorAll('[aria-label],[title],[placeholder]').forEach(function (e) {
    ['aria-label', 'title', 'placeholder'].forEach(function (k) { if (e.hasAttribute(k)) e.setAttribute(k, t(e.getAttribute(k))); });
  });
}
window.MAMI = { t: t, lang: function () { return lang; }, picker: picker, staticPage: staticPage };
})();

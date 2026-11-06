# Dựng hướng dẫn và giới thiệu (vi/en/zh) thành PDF bằng Chromium. Chạy: python3 gen_docs.py <thư-mục-ảnh-chụp> <thư-mục-ra>
import sys, os, io, base64, html, asyncio
from PIL import Image
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import content as C
SH, OUT = sys.argv[1], sys.argv[2]
os.makedirs(OUT, exist_ok=True)
LOGO = open(os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', 'server', 'web', 'logo-light.svg'), encoding='utf8').read()
IDX = {'vi': 0, 'en': 1, 'zh': 2}
LANGNAME = {'vi': 'Tiếng Việt', 'en': 'English', 'zh': '简体中文'}
TITLES = {'guide': ("Hướng dẫn cài đặt và sử dụng", "Installation and User Guide", "安装与使用指南"),
          'intro': ("Giới thiệu MAM", "Introducing MAM", "MAM 简介")}
TOC = ("Mục lục", "Contents", "目录")
FILES = {'guide': {'vi': 'MAM-Huong-dan-vi.pdf', 'en': 'MAM-Guide-en.pdf', 'zh': 'MAM-Guide-zh.pdf'},
         'intro': {'vi': 'MAM-Gioi-thieu-vi.pdf', 'en': 'MAM-Intro-en.pdf', 'zh': 'MAM-Intro-zh.pdf'}}

def img64(lang, name):
    p = os.path.join(SH, f'{lang}-{name}.png')
    im = Image.open(p).convert('RGB'); im.thumbnail((1500, 1500))
    b = io.BytesIO(); im.save(b, 'JPEG', quality=84)
    return 'data:image/jpeg;base64,' + base64.b64encode(b.getvalue()).decode()

CSS = """
@page { size: A4; margin: 18mm 17mm 18mm 17mm; }
:root{--ink:#16202e;--mute:#5a6678;--line:#d9e0ea;--blue:#1f5fd6;--green:#18a058;--soft:#f3f6fb;--warn:#fff6e0;--warnl:#e6b800}
*{box-sizing:border-box} html{font-size:10.2pt}
body{margin:0;color:var(--ink);font-family:'Inter','Noto Sans CJK SC','Noto Sans CJK HK','DejaVu Sans',sans-serif;line-height:1.55}
h1,h2,h3{font-weight:700;line-height:1.25;margin:0;text-wrap:balance}
.cover{height:255mm;display:flex;flex-direction:column;justify-content:space-between;page-break-after:always}
.cover .logo svg{width:64px;height:64px}
.cover h1{font-size:30pt;letter-spacing:-.5px;margin:18px 0 8px}
.cover .sub{font-size:13pt;color:var(--mute);max-width:130mm}
.cover .bar{height:6px;width:90px;background:linear-gradient(90deg,var(--blue),var(--green));border-radius:3px;margin:22px 0}
.cover .meta{color:var(--mute);font-size:9.5pt;line-height:1.7}
.toc{page-break-after:always} .toc h2{font-size:17pt;margin-bottom:12px}
.toc ol{list-style:none;padding:0;margin:0} .toc li{padding:6px 0;border-bottom:1px solid var(--line);font-size:11pt}
section{margin:0 0 6mm} section h2{font-size:15pt;color:var(--blue);margin:0 0 8px;padding-top:2mm;break-after:avoid}
h3.sub{font-size:11.5pt;margin:6px 0 5px;color:var(--ink)} p{margin:0 0 8px} ul,ol{margin:0 0 9px;padding-left:19px} li{margin:0 0 4px}
code,pre{font-family:'DejaVu Sans Mono',monospace;font-size:8.6pt}
pre{background:#0f172a;color:#e6edf7;padding:9px 11px;border-radius:7px;white-space:pre-wrap;word-break:break-all;margin:0 0 9px}
.note,.warn{border-radius:7px;padding:8px 11px;margin:0 0 9px;font-size:9.6pt;break-inside:avoid}
.note{background:var(--soft);border-left:4px solid var(--blue)} .warn{background:var(--warn);border-left:4px solid var(--warnl)}
figure{margin:4px 0 11px;break-inside:avoid} figure img{width:100%;border:1px solid var(--line);border-radius:7px;display:block}
figcaption{font-size:8.6pt;color:var(--mute);text-align:center;margin-top:4px}
table{width:100%;border-collapse:collapse;margin:0 0 10px;font-size:9.3pt;break-inside:avoid}
th{background:var(--soft);text-align:left;font-size:8.6pt;text-transform:none} th,td{border:1px solid var(--line);padding:5px 8px;vertical-align:top} td:first-child{font-weight:600;width:27%}
.foot{margin-top:8mm;color:var(--mute);font-size:8.6pt}
/* leaflet */
.hero{display:flex;gap:16px;align-items:center;margin-bottom:6mm}.hero svg{width:58px;height:58px;flex:none}
.hero h1{font-size:25pt;letter-spacing:-.4px}.hero p{color:var(--mute);margin:4px 0 0;font-size:11.5pt}
.grid{display:grid;grid-template-columns:1fr 1fr;gap:9px;margin-bottom:6mm}
.card{border:1px solid var(--line);border-radius:9px;padding:10px 12px;break-inside:avoid;background:#fff} .card h3{font-size:11pt;color:var(--blue);margin-bottom:3px} .card p{margin:0;font-size:9.4pt;color:#2b3648}
.cols{display:grid;grid-template-columns:1fr 1fr;gap:12px;margin-bottom:5mm} .cols h3{font-size:11pt;margin-bottom:5px}
.shot img{width:100%;border:1px solid var(--line);border-radius:8px}
"""

def pick(x, i): return x[i] if isinstance(x, tuple) else x
def esc(s): return html.escape(s, quote=False)

def render_block(b, lang, i):
    k = b[0]
    if k == 'p': return f'<p>{esc(pick(b[1], i))}</p>'
    if k in ('ul', 'ol'): return f'<{k}>' + ''.join(f'<li>{esc(pick(x, i))}</li>' for x in b[1]) + f'</{k}>'
    if k == 'h': return f'<h3 class="sub">{esc(pick(b[1], i))}</h3>'
    if k == 'code': return f'<pre>{esc(b[1])}</pre>'
    if k == 'note': return f'<div class="note">{esc(pick(b[1], i))}</div>'
    if k == 'warn': return f'<div class="warn"><b>!</b> {esc(pick(b[1], i))}</div>'
    if k == 'img': return f'<figure><img src="{img64(lang, b[1])}"><figcaption>{esc(pick(b[2], i))}</figcaption></figure>'
    if k == 'rows':
        rows = b[1]
        h = ''.join(f'<th>{esc(pick(c, i))}</th>' for c in rows[0])
        r = ''.join('<tr>' + ''.join(f'<td>{esc(pick(c, i))}</td>' for c in row) + '</tr>' for row in rows[1:])
        return f'<table><thead><tr>{h}</tr></thead><tbody>{r}</tbody></table>'
    raise ValueError(k)

def page(lang, body):
    return f'<!doctype html><html lang="{ "zh-CN" if lang=="zh" else lang}"><meta charset="utf-8"><style>{CSS}</style><body>{body}</body></html>'

def guide(lang):
    i = IDX[lang]; t = TITLES['guide'][i]
    cover = f'''<div class="cover"><div><div class="logo">{LOGO}</div><h1>MAM<br>{esc(t)}</h1><div class="bar"></div>
<div class="sub">{esc(C.INTRO_TAG[i])}</div></div>
<div class="meta">Missing App Monitor · v{C.V} · {LANGNAME[lang]}<br>© 2026 Nguyễn Tấn Tài (Missing)</div></div>'''
    toc = f'<div class="toc"><h2>{TOC[i]}</h2><ol>' + ''.join(f'<li>{esc(pick(s[0], i))}</li>' for s in C.G) + '</ol></div>'
    secs = ''.join(f'<section><h2>{esc(pick(s[0], i))}</h2>' + ''.join(render_block(b, lang, i) for b in s[1]) + '</section>' for s in C.G)
    return page(lang, cover + toc + secs)

def intro(lang):
    i = IDX[lang]
    feats = ''.join(f'<div class="card"><h3>{esc(f[0][i])}</h3><p>{esc(f[1][i])}</p></div>' for f in C.INTRO)
    fit = f'<div><h3>{esc(C.INTRO_FIT[0][i])}</h3><ul>' + ''.join(f'<li>{esc(x[i])}</li>' for x in C.INTRO_FIT[1]) + '</ul></div>'
    req = f'<div><h3>{esc(C.INTRO_REQ[0][i])}</h3><ul>' + ''.join(f'<li>{esc(x[i])}</li>' for x in C.INTRO_REQ[1]) + '</ul></div>'
    shots = ''.join(f'<div class="shot"><img src="{img64(lang, n)}"></div>' for n in ('overview', 'detail'))
    body = f'''<div class="hero">{LOGO}<div><h1>MAM · Missing App Monitor</h1><p>{esc(C.INTRO_TAG[i])}</p></div></div>
<div class="grid" style="grid-template-columns:1fr 1fr">{shots}</div>
<div class="grid">{feats}</div>
<div class="cols">{fit}{req}</div>
<div class="note"><b>{esc(C.INTRO_NOT[0][i])}.</b> {esc(C.INTRO_NOT[1][i])}</div>
<div class="foot">v{C.V} · © 2026 Nguyễn Tấn Tài (Missing)</div>'''
    return page(lang, '<div style="zoom:%s">' % ('.74' if lang=='en' else '.8') + body + '</div>')

async def main():
    from playwright.async_api import async_playwright
    async with async_playwright() as p:
        b = await p.chromium.launch(executable_path='/opt/pw-browsers/chromium')
        for kind, fn in (('guide', guide), ('intro', intro)):
            for lang in ('vi', 'en', 'zh'):
                pg = await b.new_page()
                await pg.set_content(fn(lang), wait_until='load')
                out = os.path.join(OUT, FILES[kind][lang])
                foot = '<div style="font-size:8px;color:#778;width:100%;text-align:center"><span class="pageNumber"></span> / <span class="totalPages"></span></div>' if kind == 'guide' else '<div></div>'
                await pg.pdf(path=out, format='A4', print_background=True, display_header_footer=(kind == 'guide'),
                             header_template='<div></div>', footer_template=foot, margin={'top': '18mm', 'bottom': '18mm', 'left': '17mm', 'right': '17mm'})
                await pg.close(); print(out, os.path.getsize(out) // 1024, 'KB')
        await b.close()
asyncio.run(main())

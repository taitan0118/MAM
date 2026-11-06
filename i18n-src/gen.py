import json,sys
sys.path.insert(0,'.')
from d1 import D1; from d2 import D2; from d3 import D3, P
en={};zh={}
for k,e,z in D1+D2+D3: en[k]=e; zh[k]=z
t=open('tpl.js',encoding='utf8').read()
t=t.replace('@@EN@@',json.dumps(en,ensure_ascii=False,indent=0)).replace('@@ZH@@',json.dumps(zh,ensure_ascii=False,indent=0)).replace('@@PAT@@',json.dumps([list(p) for p in P],ensure_ascii=False,indent=0))
open('../server/web/i18n.js','w',encoding='utf8').write(t)
print(len(en),'khóa')

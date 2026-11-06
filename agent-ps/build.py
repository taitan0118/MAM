#!/usr/bin/env python3
"""Ghép lib.ps1 (dùng chung với agent Go) vào main.ps1 thành MAM-Agent.ps1."""
import pathlib
here = pathlib.Path(__file__).parent
lib = (here.parent / "agent" / "lib.ps1").read_text(encoding="utf-8")
main = (here / "main.ps1").read_text(encoding="utf-8")
out = main.replace("#LIB#", lib)
# BOM UTF-8 để PowerShell 2.0 đọc đúng tiếng Việt; xuống dòng kiểu Windows
(here / "MAM-Agent.ps1").write_bytes(("\ufeff" + out.replace("\r\n", "\n").replace("\n", "\r\n")).encode("utf-8"))
print("OK", here / "MAM-Agent.ps1")

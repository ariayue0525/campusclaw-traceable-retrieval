#!/usr/bin/env python3
import html
import importlib.util
import os
import pathlib
import time
import traceback

from common import Context


ROOT = pathlib.Path(__file__).resolve().parent
ORDER = [
    "cases/db/schema/CHECK.py",
    "cases/api/login/POST.py",
    "cases/api/search/POST.py",
    "cases/api/agents/POST.py",
    "cases/api/skills/POST.py",
    "cases/api/prompt-templates/POST.py",
    "cases/api/conversations/POST.py",
    "cases/api/conversations/STREAM.py",
    "cases/api/mcp/POST.py",
    "cases/api/swagger/GET.py",
]


def load_env():
    path = ROOT.parent / ".env"
    if not path.exists():
        return
    for line in path.read_text().splitlines():
        if not line or line.lstrip().startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        os.environ.setdefault(key, value)


def main():
    load_env()
    ctx, rows = Context(), []
    for index, relative in enumerate(ORDER):
        path = ROOT / relative
        spec = importlib.util.spec_from_file_location(f"case_{index}", path)
        module = importlib.util.module_from_spec(spec)
        started = time.time()
        try:
            spec.loader.exec_module(module)
            detail = module.run(ctx) or "通过"
            state = "SKIP" if str(detail).startswith("SKIP:") else "PASS"
        except Exception:
            state, detail = "FAIL", traceback.format_exc()
        rows.append((relative, state, time.time() - started, detail))
        print(f"{state:4} {relative} — {detail}")
    report = ROOT / "report.html"
    body = "".join(f"<tr class='{s.lower()}'><td>{html.escape(p)}</td><td>{s}</td><td>{d:.3f}s</td><td><pre>{html.escape(str(x))}</pre></td></tr>" for p,s,d,x in rows)
    report.write_text(f"<!doctype html><meta charset='utf-8'><title>CampusClaw AI Platform API Test Report</title><style>body{{font:14px system-ui;margin:30px}}table{{border-collapse:collapse;width:100%}}td,th{{border:1px solid #ddd;padding:8px;vertical-align:top}}.pass td:nth-child(2){{color:green}}.fail td:nth-child(2){{color:#b00}}.skip td:nth-child(2){{color:#986f0b}}pre{{white-space:pre-wrap;margin:0}}</style><h1>CampusClaw AI 平台接口验收</h1><p>{time.strftime('%Y-%m-%d %H:%M:%S')}</p><table><tr><th>测试脚本</th><th>结果</th><th>耗时</th><th>断言/错误</th></tr>{body}</table>", encoding="utf-8")
    failures = sum(1 for _, state, _, _ in rows if state == "FAIL")
    print(f"报告：{report}；通过 {sum(1 for _,s,_,_ in rows if s=='PASS')}，跳过 {sum(1 for _,s,_,_ in rows if s=='SKIP')}，失败 {failures}")
    raise SystemExit(1 if failures else 0)


if __name__ == "__main__":
    main()

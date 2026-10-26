#!/usr/bin/env python3
"""Render the repository's platform audit Markdown as a Chinese PDF."""

from html import escape
import os
from pathlib import Path
import re

from reportlab.lib import colors
from reportlab.lib.enums import TA_CENTER, TA_LEFT
from reportlab.lib.pagesizes import A4
from reportlab.lib.styles import ParagraphStyle
from reportlab.lib.units import mm
from reportlab.pdfbase import pdfmetrics
from reportlab.pdfbase.ttfonts import TTFont
from reportlab.platypus import (
    HRFlowable,
    Paragraph,
    Preformatted,
    SimpleDocTemplate,
    Spacer,
)


ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / "docs/个人项目平台功能核验报告.md"
TARGET = ROOT / "output/pdf/CampusClaw_个人项目平台功能核验报告.pdf"


def markup(line: str) -> str:
    """Convert the small inline subset used by the audit into ReportLab markup."""
    pieces = re.split(r"(`[^`]+`|\*\*[^*]+\*\*|<https?://[^>]+>)", line)
    out = []
    for piece in pieces:
        if piece.startswith("`") and piece.endswith("`"):
            out.append(f'<font color="#9B1C31">{escape(piece[1:-1])}</font>')
        elif piece.startswith("**") and piece.endswith("**"):
            out.append(f"<b>{escape(piece[2:-2])}</b>")
        elif piece.startswith("<http") and piece.endswith(">"):
            url = escape(piece[1:-1], quote=True)
            out.append(f'<link href="{url}" color="#8A1E31">{url}</link>')
        else:
            out.append(escape(piece))
    return "".join(out)


def styles():
    font = Path(os.environ.get("CAMPUSCLAW_PDF_FONT", "/System/Library/Fonts/Supplemental/Arial Unicode.ttf"))
    if not font.is_file():
        raise SystemExit("Set CAMPUSCLAW_PDF_FONT to an installed Unicode TrueType font")
    pdfmetrics.registerFont(TTFont("ArialUnicode", str(font)))
    pdfmetrics.registerFontFamily("ArialUnicode", normal="ArialUnicode", bold="ArialUnicode")
    base = dict(fontName="ArialUnicode")
    return {
        "title": ParagraphStyle("title", **base, fontSize=20, leading=27, alignment=TA_CENTER, spaceAfter=14),
        "h2": ParagraphStyle("h2", **base, fontSize=12.5, leading=18, textColor=colors.HexColor("#8A1E31"), spaceBefore=16, spaceAfter=8),
        "body": ParagraphStyle("body", **base, fontSize=9.3, leading=16, alignment=TA_LEFT, spaceAfter=7),
        "list": ParagraphStyle("list", **base, fontSize=9.3, leading=16, leftIndent=16, firstLineIndent=-12, spaceAfter=6),
        "code": ParagraphStyle("code", **base, fontSize=8.2, leading=13, leftIndent=10, rightIndent=10, backColor=colors.HexColor("#F4F1EE"), spaceBefore=5, spaceAfter=10),
    }


def build_story(source: str):
    style = styles()
    story = []
    para = []
    code = []
    in_code = False

    def flush_para():
        if para:
            story.append(Paragraph(markup(" ".join(para)), style["body"]))
            para.clear()

    for line in source.splitlines():
        stripped = line.strip()
        if stripped.startswith("```"):
            flush_para()
            if in_code:
                story.append(Preformatted("\n".join(code), style["code"], maxLineLength=95))
                code.clear()
            in_code = not in_code
            continue
        if in_code:
            code.append(line)
            continue
        if not stripped:
            flush_para()
            continue
        if stripped.startswith("# "):
            flush_para()
            story.append(Paragraph(markup(stripped[2:]), style["title"]))
            story.append(HRFlowable(width="100%", thickness=1, color=colors.HexColor("#B73548")))
            story.append(Spacer(1, 8))
        elif stripped.startswith("## "):
            flush_para()
            story.append(Paragraph(markup(stripped[3:]), style["h2"]))
        elif stripped.startswith("- ") or re.match(r"^\d+\. ", stripped):
            flush_para()
            body = stripped[2:] if stripped.startswith("- ") else re.sub(r"^\d+\. ", "", stripped)
            marker = "•" if stripped.startswith("- ") else stripped.split(".", 1)[0] + "."
            story.append(Paragraph(f"{marker} {markup(body)}", style["list"]))
        else:
            para.append(stripped)
    flush_para()
    return story


def footer(canvas, document):
    canvas.saveState()
    width, _ = A4
    canvas.setStrokeColor(colors.HexColor("#D9D3CE"))
    canvas.line(18 * mm, 17 * mm, width - 18 * mm, 17 * mm)
    canvas.setFont("ArialUnicode", 8)
    canvas.setFillColor(colors.HexColor("#68727D"))
    canvas.drawString(18 * mm, 11 * mm, "CampusClaw · 功能核验 · 2026-10-26")
    canvas.drawRightString(width - 18 * mm, 11 * mm, str(document.page))
    canvas.restoreState()


def main():
    TARGET.parent.mkdir(parents=True, exist_ok=True)
    document = SimpleDocTemplate(
        str(TARGET), pagesize=A4, leftMargin=19 * mm, rightMargin=19 * mm,
        topMargin=19 * mm, bottomMargin=23 * mm, title="CampusClaw 个人项目平台功能核验报告",
        author="CampusClaw 项目核验",
    )
    document.build(build_story(SOURCE.read_text(encoding="utf-8")), onFirstPage=footer, onLaterPages=footer)
    print(TARGET)


if __name__ == "__main__":
    main()

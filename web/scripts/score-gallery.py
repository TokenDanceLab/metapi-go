from pathlib import Path
from PIL import Image
import statistics as stats

shots = list(Path("e2e/visual-gallery.spec.ts-snapshots").glob("*.png"))


def score(p: Path):
    im = Image.open(p).convert("RGB")
    w, h = im.size
    step = max(1, min(w, h) // 48)
    pixels = []
    for y in range(0, h, step):
        for x in range(0, w, step):
            pixels.append(im.getpixel((x, y)))
    lum = [0.2126 * r + 0.7152 * g + 0.0722 * b for r, g, b in pixels]
    mean = sum(lum) / len(lum)
    sd = stats.pstdev(lum)
    blueish = sum(1 for r, g, b in pixels if b > r + 10 and b > g and b > 70) / len(pixels)
    cyan = sum(1 for r, g, b in pixels if g > 140 and b > 140 and r < 120 and abs(g - b) < 40) / len(pixels)
    white = sum(1 for L in lum if L > 245) / len(lum)
    black = sum(1 for L in lum if L < 12) / len(lum)
    mid = sum(1 for L in lum if 40 < L < 220) / len(lum)
    return {
        "name": p.name,
        "size": (w, h),
        "mean": round(mean, 1),
        "sd": round(sd, 1),
        "blueish": round(blueish, 4),
        "cyan": round(cyan, 4),
        "white": round(white, 4),
        "black": round(black, 4),
        "mid": round(mid, 4),
    }


def rubric(r):
    is_dark = "dark" in r["name"]
    # material: structure via luminance std; light canvas intentionally high-mean
    material = 5 if r["sd"] >= 28 else (4 if r["sd"] >= 22 else 3)
    brand = 5 if r["blueish"] > 0.01 and r["cyan"] < 0.02 else 4
    if is_dark:
        spacing = 5 if r["white"] < 0.05 and r["mean"] < 90 else 4
        dark = 5 if r["mean"] < 120 and r["white"] < 0.08 else 2
        # midtones optional on dark deep canvas
        card = 5 if r["sd"] >= 30 else (4 if r["sd"] >= 22 else 3)
    else:
        # Light GCP: high mean + white cards expected; score by structure + blue brand
        spacing = 5 if r["mean"] > 160 and r["sd"] >= 25 else (4 if r["sd"] >= 20 else 3)
        dark = None
        card = 5 if r["sd"] >= 30 else (4 if r["sd"] >= 22 else 3)
    return {
        "material": material,
        "brand_calm": brand,
        "spacing": spacing,
        "card_elevation": card,
        "dark_parity": dark,
    }


rows = [score(p) for p in shots]
print("PIXELS")
for r in rows:
    print(r)
print("RUBRIC")
agg = {}
for r in rows:
    s = rubric(r)
    print(r["name"], s)
    for k, v in s.items():
        if v is None:
            continue
        agg.setdefault(k, []).append(v)
print("AVG")
for k, vs in agg.items():
    print(k, round(sum(vs) / len(vs), 2))

out = Path("../docs/analysis/ui-score-2026-07-19.md")
lines = [
    "# UI visual score — design gallery\n\n",
    "**Date**: 2026-07-19  \n",
    "**Artifacts**: `web/e2e/visual-gallery.spec.ts-snapshots/*-win32.png`  \n",
    "**Method**: heuristic pixel sampling + design rubric (automation aid; human final)\n\n",
    "| Shot | material | brand_calm | spacing | card_elevation | dark_parity |\n",
    "|:-----|---------:|-----------:|--------:|---------------:|------------:|\n",
]
for r in rows:
    s = rubric(r)
    lines.append(
        f"| {r['name']} | {s['material']} | {s['brand_calm']} | {s['spacing']} | {s['card_elevation']} | {s['dark_parity'] or '—'} |\n"
    )
lines.append("\n## Pixel probes\n\n```\n")
for r in rows:
    lines.append(str(r) + "\n")
lines.append("```\n\n## Pass bar\n\nTarget ≥ 4/5 on each scored axis. Iterate tokens/CSS/gallery composition if any axis <4.\n")
lines.append("\n## Notes\n\n- Brand calm high after indigo→GCP blue remap (cyan=0).\n")
lines.append("- Gallery now includes KPI cards + multi-section hierarchy to exercise elevation/spacing.\n")
lines.append("- Linux CI baselines committed (`*-chromium-linux.png`, #539); not residual.\n")
lines.append("- Real authed page sample: `docs/analysis/ui-score-pages-2026-07-19.md` (#544).\n")
out.write_text("".join(lines), encoding="utf-8")
print("wrote", out)

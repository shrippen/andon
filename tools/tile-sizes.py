#!/usr/bin/env python3
"""Renders every tile type in 1x1, two columns, two rows and 2x2 and lists
what each size leaves empty or cuts off.

Usage: start the demo (./start.sh demo, default http://localhost:8080), then
  <shrippen.github.io>/demo/tools/.venv/bin/python tools/tile-sizes.py [base-url] [--shots DIR]

Each type comes from its gallery sample (/widget-sample/<type>) and is set
into a section grid like a board's: three columns, neighbours as tall as
the 1x1 tile, so "two rows" is the height the board would give it.

  type            1x1 over  2w right  2h bottom  2x2 bottom
  backups         -         4 %       58 %  !    61 %  !

Flags (!): content ends before 60 % of the card (empty), or runs past it
(over). Types whose sample only shows an error are listed apart.
"""

import json
import sys

from playwright.sync_api import sync_playwright

LOGIN = ("mara@studio-weber.example.test", "demo-password-1")  # internal/services/seed DemoUser
SIZES = [("1x1", 1, 1), ("2w", 2, 1), ("2h", 1, 2), ("2x2", 2, 2)]
EMPTY = 0.4  # share of the card left empty that counts as a gap

# Builds one section per type and size, waits, measures; runs in the page.
MEASURE = r"""
async (types) => {
  const root = document.querySelector('main');
  root.innerHTML = '';
  root.className = 'board-main';
  root.style.cssText = 'display:block;width:760px;max-width:none;margin:0;padding:8px';

  // andon.js turns data-style into style after a swap; do the same here.
  const styles = (el) => el.querySelectorAll('[data-style]').forEach((e) => {
    e.setAttribute('style', (e.getAttribute('style') || '') + ';' + e.getAttribute('data-style'));
  });

  const place = (type, body, cols, rows, height) => {
    const sec = document.createElement('section'); sec.className = 'dsec';
    const grid = document.createElement('div'); grid.className = 'body dsec-body';
    grid.setAttribute('data-size', 'medium'); grid.style.cssText = '--cols:3;width:744px';
    const slot = document.createElement('div'); slot.className = 'tile-slot w-' + type;
    if (rows > 1) slot.setAttribute('data-rows', rows);
    if (cols > 1) slot.setAttribute('data-cols', cols);
    slot.innerHTML = '<article class="card"><h3 class="card-title">' + type + '</h3><div class="card-body">' + body + '</div></article>';
    styles(slot);
    grid.appendChild(slot);
    for (let i = 0; i < rows; i++) {
      const ghost = document.createElement('div');
      ghost.style.cssText = 'height:' + height + 'px;grid-column:' + (cols + 1);
      grid.appendChild(ghost);
    }
    sec.appendChild(grid); root.appendChild(sec);
    return slot;
  };

  const measure = (slot) => {
    const card = slot.querySelector('.card').getBoundingClientRect();
    const body = slot.querySelector('.card-body');
    let right = -1, bottom = -1, over = false;
    body.querySelectorAll('*').forEach((e) => {
      const text = [...e.childNodes].some((n) => n.nodeType === 3 && n.textContent.trim());
      if (!text && !['svg', 'img', 'canvas', 'iframe', 'i'].includes(e.tagName.toLowerCase())) return;
      const b = e.getBoundingClientRect();
      if (!b.width || !b.height) return;
      right = Math.max(right, b.right); bottom = Math.max(bottom, b.bottom);
      if (b.right > card.right + 2 || b.bottom > card.bottom + 2) over = true;
    });
    return {
      w: Math.round(card.width), h: Math.round(card.height),
      right: right < 0 ? 1 : +((card.right - right) / card.width).toFixed(2),
      bottom: bottom < 0 ? 1 : +((card.bottom - bottom) / card.height).toFixed(2),
      over: over || body.scrollWidth > body.clientWidth + 2,
    };
  };

  const out = {};
  for (const [type, url] of types) {
    // One row and two rows: a taller tile shows more entries (widgets.ForRows).
    const bodies = {};
    for (const rows of [1, 2]) bodies[rows] = await (await fetch(url + '&rows=' + rows)).text();
    out[type] = {error: bodies[1].length < 600 && /Fehler|Error/.test(bodies[1])};
    let height = 90;
    for (const [name, cols, rows] of %SIZES%) {
      const slot = place(type, bodies[rows], cols, rows, height);
      await new Promise((r) => setTimeout(r, 50));
      out[type][name] = measure(slot);
      if (name === '1x1') height = Math.max(out[type][name].h, 90);
      slot.closest('section').dataset.shot = type + '_' + name;
    }
  }
  return out;
}
""".replace("%SIZES%", json.dumps(SIZES))


def main():
    args = [a for a in sys.argv[1:] if not a.startswith("--")]
    base = args[0] if args else "http://localhost:8080"
    shots = sys.argv[sys.argv.index("--shots") + 1] if "--shots" in sys.argv else None

    with sync_playwright() as p:
        browser = p.chromium.launch()
        page = browser.new_page(viewport={"width": 1100, "height": 900})
        page.goto(base + "/login")
        page.fill("#email", LOGIN[0])
        page.fill("#password", LOGIN[1])
        page.keyboard.press("Enter")
        page.wait_for_load_state("networkidle")

        # The gallery lists every type with its sample's URL.
        page.goto(base + "/widgets/new")
        page.wait_for_load_state("networkidle")
        types = page.evaluate("""[...new Set([...document.querySelectorAll('[hx-get*="widget-sample"]')]
            .map(e => e.getAttribute('hx-get')))].map(u => [u.split('/')[2].split('?')[0], u])""")

        # Any board page brings the app's styles.
        page.goto(base + "/")
        page.wait_for_load_state("networkidle")
        result = page.evaluate(MEASURE, types)
        if shots:
            for section in page.locator("section[data-shot]").all():
                section.screenshot(path=f"{shots}/{section.get_attribute('data-shot')}.png")
        browser.close()

    report(result)


def report(result):
    """Prints one line per type, then the types to look at."""
    gaps, errors = [], []
    print(f"{'type':22} {'1x1 over':9} {'2w right':9} {'2h bottom':10} {'2x2 bottom':10}")
    for name, sizes in sorted(result.items()):
        if sizes["error"]:
            errors.append(name)
            continue
        flag = lambda v: f"{round(v * 100):>3} %{'  !' if v >= EMPTY else '   '}"
        over = "over !" if sizes["1x1"]["over"] else "-"
        print(f"{name:22} {over:9} {flag(sizes['2w']['right']):9} {flag(sizes['2h']['bottom']):10} {flag(sizes['2x2']['bottom']):10}")
        if sizes["1x1"]["over"] or sizes["2h"]["bottom"] >= EMPTY:
            gaps.append(name)
    print(f"\n{len(gaps)} of {len(result) - len(errors)} types to look at: {', '.join(gaps)}")
    if errors:
        print(f"Sample shows an error, not measured: {', '.join(errors)}")


if __name__ == "__main__":
    main()

#!/usr/bin/env python3
"""Works on /vat, reloads the page and checks that the draft put every
change back: a typed vendor, a taken case, a split with a part and a
scan found by a search. Then writes one case alone ("Nur diesen
prüfen"): its review shows below the desk, goes stale on a change, and
the write (refused by the demo) leaves the other changes in the draft.
Discards the draft at the end. Last, every other bench with cases in
the demo: a row's first text field and its tick survive a reload.

Usage: start the demo (./start.sh demo, default http://localhost:8080), then
  <shrippen.github.io>/demo/tools/.venv/bin/python tools/bench-draft.py [base-url]

The split's slots and search hits load by htmx; put back before htmx
started, they never came:

  slots: [] (want ['', 'expense', '', '', '', '', '', ''])
  scan: [] (want ['311'])

Exits 1 when a value is lost or the page threw.
"""

import sys

from playwright.sync_api import TimeoutError as PlaywrightTimeout
from playwright.sync_api import sync_playwright

LOGIN = ("mara@studio-weber.example.test", "demo-password-1")  # internal/services/seed DemoUser

# Demo debits (sources/demo_bank.go): a new expense, one to split, one
# to give a scan from the search.
CREATE, SPLIT, SCAN = "demo-b-studio", "demo-b-office", "demo-b-music"
VENDOR, QUERY = "Draftwerk GmbH", "Rechnung"
SAVE_MS = 1500  # andon.js DRAFT_WAIT_MS and the request
LOAD_MS = 5000  # a split or a search after the reload


def uses(page, k):
    return page.eval_on_selector_all(f'[data-desk="{k}"] [data-split-use]', "els => els.map(e => e.value)")


def work(page):
    page.click(f'.bench-entry[data-case="{CREATE}"]')
    page.fill(f'[name="vendor_{CREATE}"]', VENDOR)
    page.click(f'[data-desk="{CREATE}"] [data-take-next]')

    page.click(f'.bench-entry[data-case="{SPLIT}"]')
    page.click(f'[data-desk="{SPLIT}"] [data-set-act="split"]')
    page.wait_for_selector(f'[data-desk="{SPLIT}"] [data-split]')
    page.click(f'[data-desk="{SPLIT}"] [data-split-add]')

    page.click(f'.bench-entry[data-case="{SCAN}"]')
    page.fill(f'[name="q_scan_{SCAN}"]', QUERY)
    page.click(f'[data-desk="{SCAN}"] [data-scan-search] button')
    page.wait_for_selector(f'#found-scan-{SCAN} input[type=radio]', state="attached")
    scan = page.eval_on_selector_all(f'#found-scan-{SCAN} input[type=radio]', "els => els.map(e => e.value)")[-1]
    page.check(f'#found-scan-{SCAN} input[value="{scan}"]')
    page.wait_for_timeout(SAVE_MS)
    return {"vendor": VENDOR, "taken": True, "act": "split", "slots": uses(page, SPLIT), "scan": [scan]}


def wait(page, selector):
    try:
        page.wait_for_selector(selector, state="attached", timeout=LOAD_MS)
    except PlaywrightTimeout:
        pass  # the comparison names what is missing


def seen(page):
    wait(page, f'[data-desk="{SPLIT}"] [data-split]')
    wait(page, f'[name="scan_{SCAN}"]:checked')
    return {
        "vendor": page.input_value(f'[name="vendor_{CREATE}"]'),
        "taken": page.is_checked(f'[data-desk="{CREATE}"] input[data-pick]'),
        "act": page.input_value(f'[data-desk="{SPLIT}"] input[data-act]'),
        "slots": uses(page, SPLIT),
        "scan": page.eval_on_selector_all(f'[name="scan_{SCAN}"]:checked', "els => els.map(e => e.value)"),
    }


def one(page):
    """Writes CREATE alone; returns what went wrong."""
    review = f'[data-desk="{CREATE}"] [hx-post="/vat/review"]'
    box = f"#one-{CREATE}"
    page.click(f'.bench-entry[data-case="{CREATE}"]')
    page.click(review)
    page.wait_for_selector(box + " .card")
    page.fill(f'[name="vendor_{CREATE}"]', VENDOR + " AG")
    wrong = []
    if page.inner_html(box).strip():
        wrong.append("one: review kept after a change")

    # Another desk's review leaves only that one on the page.
    page.click(review)
    page.wait_for_selector(box + " .card")
    page.click(f'.bench-entry[data-case="{SCAN}"]')
    page.click(f'[data-desk="{SCAN}"] [hx-post="/vat/review"]')
    page.wait_for_selector(f"#one-{SCAN} .card")
    if page.inner_html(box).strip():
        wrong.append("one: another desk's review kept")

    page.click(f'.bench-entry[data-case="{CREATE}"]')
    page.click(review)
    page.wait_for_selector(box + " .card")

    # Enter in a field (no change, so the review stays) must not write
    # the reviewed case: the bench's own default button goes.
    page.focus(f'[name="note_{CREATE}"]')
    with page.expect_navigation():
        page.keyboard.press("Enter")
    page.wait_for_load_state("networkidle")
    if "err_demo" in page.url:
        wrong.append("one: Enter wrote the reviewed case")
        return wrong
    page.goto(page.url if "/vat?" in page.url else page.url.split("/vat")[0] + "/vat")
    page.wait_for_load_state("networkidle")

    page.click(f'.bench-entry[data-case="{CREATE}"]')
    page.click(review)
    page.wait_for_selector(box + " .card")
    page.click(box + ' button[form="vat-one"]')
    page.wait_for_url("**error=vatcheck.err_demo**")
    page.wait_for_load_state("networkidle")
    wait(page, f'[data-desk="{SPLIT}"] [data-split]')
    if page.input_value(f'[data-desk="{SPLIT}"] input[data-act]') != "split":
        wrong.append("one: the other cases' draft lost")
    return wrong


# The other benches with a draft (web/benches.go); vendors has no case
# in the demo.
OTHERS = ["euer", "week", "inventory", "dunning", "purchases", "subs", "updates", "categorize", "feeds"]
ROW = "form[data-draft] [data-draft-case]"
TEXT = "input[name]:not([type]), input[type=text][name], textarea[name]"


def others(page, base):
    """Changes a row on each bench, reloads; returns what was lost."""
    wrong = []
    for bench in OTHERS:
        page.goto(f"{base}/{bench}")
        page.wait_for_load_state("networkidle")
        rows = [r for r in page.query_selector_all(ROW) if r.query_selector('input[name="pick"]')]
        row = rows[0] if rows else None
        if row is None:
            print(f"{bench}: no case in the demo, skipped")
            continue
        k = row.get_attribute("data-draft-case")
        box = f'{ROW}[data-draft-case="{k}"]'
        text = page.query_selector(f"{box} :is({TEXT})")
        tick = page.query_selector(f'{box} input[name="pick"]')
        typed = ticked = None
        if text and text.is_visible():
            text.fill("Entwurf " + bench)
            typed = text.get_attribute("name")
        if tick and tick.is_visible():
            ticked = not tick.is_checked()
            page.click(f'{box} label:has(input[name="pick"])')  # the styled box takes the click
        page.wait_for_timeout(SAVE_MS)
        page.reload()
        page.wait_for_load_state("networkidle")
        if typed and page.input_value(f'{box} [name="{typed}"]') != "Entwurf " + bench:
            wrong.append(f"{bench}: {typed} lost")
        if ticked is not None and page.is_checked(f'{box} input[name="pick"]') != ticked:
            wrong.append(f"{bench}: tick lost")
        if not page.query_selector("[data-draft-note]"):
            wrong.append(f"{bench}: no draft note")
        else:
            page.click("[data-draft-note] button")
            page.wait_for_load_state("networkidle")
    return wrong


def main():
    base = sys.argv[1] if len(sys.argv) > 1 else "http://localhost:8080"
    errors = []
    with sync_playwright() as p:
        browser = p.chromium.launch()
        page = browser.new_page(viewport={"width": 1440, "height": 960})
        page.on("pageerror", lambda e: errors.append(str(e)))
        page.goto(base + "/login")
        page.fill("#email", LOGIN[0])
        page.fill("#password", LOGIN[1])
        page.press("#password", "Enter")
        page.wait_for_load_state("networkidle")
        page.goto(base + "/vat")
        page.wait_for_load_state("networkidle")

        want = work(page)
        page.reload()
        page.wait_for_load_state("networkidle")
        got = seen(page)
        wrong = one(page)

        page.click("[data-draft-note] button")
        page.wait_for_url("**note=**")
        wrong += others(page, base)
        browser.close()

    lost = {k: (got[k], v) for k, v in want.items() if got[k] != v}
    for k, (g, w) in lost.items():
        print(f"{k}: {g} (want {w})")
    for w in wrong:
        print(w)
    for e in errors:
        print("error:", e)
    bad = lost or wrong or errors
    print("" if bad else "ok")
    sys.exit(1 if bad else 0)


if __name__ == "__main__":
    main()

#!/usr/bin/env python3
"""Works on /vat, reloads the page and checks that the draft put every
change back: a typed vendor, a taken case, a split with a part and a
scan found by a search. Discards the draft at the end.

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

        page.click("[data-draft-note] button")
        page.wait_for_url("**note=**")
        browser.close()

    lost = {k: (got[k], v) for k, v in want.items() if got[k] != v}
    for k, (g, w) in lost.items():
        print(f"{k}: {g} (want {w})")
    for e in errors:
        print("error:", e)
    print("ok" if not lost and not errors else "")
    sys.exit(1 if lost or errors else 0)


if __name__ == "__main__":
    main()

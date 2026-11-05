#!/usr/bin/env python3
"""Works the VAT desk the way the owner does and checks what it says:
dropping a scan gives the expense its bank day back, an unsorted entry
also after a reload that put the draft back; that reload keeps a bank
day the user clicked; an unsorted entry cannot be taken before it is
sorted, and "Später" leaves its label.

Usage: start the demo (./start.sh demo, default http://localhost:8080), then
  <shrippen.github.io>/demo/tools/.venv/bin/python tools/bench-desk.py [base-url]

Before the fix:

  day without a scan: 2026-10-23 (want 2026-10-24)
  day after a reload without a scan: 2026-10-16 (want 2026-10-24)
  bank day after a reload: 2026-10-16 (want 2026-10-24)
  take enabled while unsorted
  label after later: '' (want 'Noch unsortiert')

Exits 1 when one differs or the page threw.
"""

import sys

from playwright.sync_api import sync_playwright

LOGIN = ("mara@studio-weber.example.test", "demo-password-1")  # internal/services/seed DemoUser

# Demo debits (sources/demo_bank.go): one with a proposed scan, one unsorted.
SCANNED, UNSORTED = "demo-b-scan", "demo-p-ticket"
RECEIPT_DAY = "2020-01-02"  # far from any bank day
QUERY = "Rechnung"
SAVE_MS = 1500  # andon.js DRAFT_WAIT_MS and the request


def scan_day(page):
    """The bank day comes back when the picked scan is dropped."""
    k = SCANNED
    page.click(f'.bench-entry[data-case="{k}"]')
    day = page.locator(f'[name="day_{k}"]')
    bank = day.get_attribute("data-bank-day") or ""

    # A scan whose day differs from the bank's: the demo's may match.
    page.evaluate(
        """([k, d]) => {
          var r = document.querySelector('[data-desk="' + k + '"] input[data-part=scan][value]:not([value=""])');
          r.setAttribute("data-day", d);
          r.closest("details").open = true;
          r.setAttribute("data-test-scan", "");
        }""",
        [k, RECEIPT_DAY],
    )
    none = page.locator(f'[data-desk="{k}"] input[name="scan_{k}"][value=""]')
    none.check(force=True)  # the proposed scan may be checked already
    page.locator(f'[data-desk="{k}"] input[data-test-scan]').check(force=True)
    took = day.input_value()
    none.check(force=True)
    wrong = []
    if took != RECEIPT_DAY:
        wrong.append(f"day with a scan: {took} (want {RECEIPT_DAY})")
    if not bank or day.input_value() != bank:
        wrong.append(f"day without a scan: {day.input_value()} (want {bank})")
    return wrong


def open_scans(page, k):
    page.evaluate("""k => document.querySelector('[data-desk="' + k + '"] aside:not([hidden]) details.fold').open = true""", k)


def pick_off_bank(page, k, bank):
    """Picks a search hit dated off the bank day; its value, or None."""
    open_scans(page, k)
    page.fill(f'[name="q_scan_{k}"]', QUERY)
    page.click(f'[data-desk="{k}"] [data-scan-search] button')
    hits = f'#found-scan-{k} input[type=radio]'
    page.wait_for_selector(hits, state="attached")
    other = page.evaluate(
        """([sel, bank]) => {
          var r = [].find.call(document.querySelectorAll(sel), e => e.getAttribute("data-day") && e.getAttribute("data-day") !== bank);
          return r && r.value;
        }""",
        [hits, bank],
    )
    if other:
        page.check(f'#found-scan-{k} input[value="{other}"]')
    return other


def reload_with(page, k, scan):
    """Saves the draft, reloads and waits until the scan is back."""
    page.wait_for_timeout(SAVE_MS)
    page.reload()
    page.wait_for_load_state("networkidle")
    page.wait_for_selector(f'[name="scan_{k}"][value="{scan}"]:checked', state="attached")
    page.click(f'.bench-entry[data-case="{k}"]')


def scan_reload(page):
    """A scan picked before a reload still gives way to the bank day."""
    k = SCANNED
    page.click(f'.bench-entry[data-case="{k}"]')
    bank = page.get_attribute(f'[name="day_{k}"]', "data-bank-day")
    other = pick_off_bank(page, k, bank)
    if not other:
        return [f"demo search has no scan off the bank day {bank}"]
    reload_with(page, k, other)
    open_scans(page, k)
    page.check(f'[data-desk="{k}"] input[name="scan_{k}"][value=""]')
    got = page.input_value(f'[name="day_{k}"]')
    return [] if got == bank else [f"day after a reload without a scan: {got} (want {bank})"]


def bank_kept(page):
    """The bank day clicked against the scan's survives a reload."""
    k = SCANNED
    page.click(f'.bench-entry[data-case="{k}"]')
    bank = page.get_attribute(f'[name="day_{k}"]', "data-bank-day")
    other = pick_off_bank(page, k, bank)
    if not other:
        return [f"demo search has no scan off the bank day {bank}"]
    page.click(f'[data-suggest-for="day-{k}"]:not([data-scan-src]) button')
    reload_with(page, k, other)
    got = page.input_value(f'[name="day_{k}"]')
    return [] if got == bank else [f"bank day after a reload: {got} (want {bank})"]


def unsorted(page):
    """Nothing to take before a choice; the label stays through "Später"."""
    k = UNSORTED
    page.click(f'.bench-entry[data-case="{k}"]')
    label = page.locator(f'.bench-entry[data-case="{k}"] [data-act-label]')
    want = label.inner_text()
    wrong = []
    if not page.locator(f'[data-desk="{k}"] [data-take-next]').is_disabled():
        wrong.append("take enabled while unsorted")
    page.click(f'[data-desk="{k}"] [data-later]')
    if label.inner_text() != want:
        wrong.append(f"label after later: {label.inner_text()!r} (want {want!r})")
    return wrong


def discard(page):
    note = page.locator("[data-draft-note] button")
    if note.count() and note.first.is_visible():
        note.first.click()
        page.wait_for_url("**note=**")
        page.goto(page.url.split("?")[0])
        page.wait_for_load_state("networkidle")


def main():
    base = sys.argv[1] if len(sys.argv) > 1 else "http://localhost:8080"
    errors = []
    with sync_playwright() as p:
        browser = p.chromium.launch()
        page = browser.new_page(viewport={"width": 1600, "height": 1000})
        page.on("pageerror", lambda e: errors.append(str(e)))
        page.goto(base + "/login")
        page.fill("#email", LOGIN[0])
        page.fill("#password", LOGIN[1])
        page.press("#password", "Enter")
        page.wait_for_load_state("networkidle")
        page.goto(base + "/vat")
        page.wait_for_load_state("networkidle")

        discard(page)  # a failed run leaves its draft
        wrong = scan_day(page) + scan_reload(page) + bank_kept(page) + unsorted(page)

        page.wait_for_timeout(SAVE_MS)
        discard(page)
        browser.close()

    for w in wrong:
        print(w)
    for e in errors:
        print("error:", e)
    print("" if wrong or errors else "ok")
    sys.exit(1 if wrong or errors else 0)


if __name__ == "__main__":
    main()

#!/usr/bin/env python3
"""Works the VAT desk the way the owner does and checks what it says:
dropping a scan gives the expense its bank day back, an unsorted entry
also after a reload that put the draft back and a new search; a date
the user set (bank day clicked, day typed) stays through a reload and
another scan; a second search keeps the picked hit, also through a
reload; a picked scan gone meanwhile leaves no scan and the bank day;
an unsorted entry cannot be taken before it is
sorted, and "Später" leaves its label.

Usage: start the demo (./start.sh demo, default http://localhost:8080), then
  <shrippen.github.io>/demo/tools/.venv/bin/python tools/bench-desk.py [base-url]

Before the fix:

  day without a scan: 2026-10-23 (want 2026-10-24)
  day after a reload without a scan: 2026-10-16 (want 2026-10-24)
  bank day after a reload: 2026-10-16 (want 2026-10-24)
  day after a new search without a scan: 2026-10-16 (want 2026-10-24)
  clicked bank day after another scan: 2026-10-16 (want 2026-10-24)
  scan after a second search: none checked (want 201)
  scan after a second search and a reload: none checked (want 201)
  scan gone after a reload: none checked, day 2026-10-16 (want "Ohne Beleg", 2026-10-24)
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
QUERY, OTHER_QUERY = "Rechnung", "Bahn"
TYPED_DAY, TYPED_KEYS = "2026-10-10", "10102026"  # same in day-first and month-first fields
SWAP_MS = 300  # htmx settles the hits
RESTORE_MS = 1500  # a draft's search back after a reload
SAVE_MS = 3000  # andon.js DRAFT_WAIT_MS, the request and a slow disk


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
    none.check()  # the proposed scan may be checked already
    page.locator(f'[data-desk="{k}"] input[data-test-scan]').check()
    took = day.input_value()
    none.check()
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


def search_after_reload(page):
    """The old hit gone by a new search, its date still gives way."""
    k = SCANNED
    page.click(f'.bench-entry[data-case="{k}"]')
    bank = page.get_attribute(f'[name="day_{k}"]', "data-bank-day")
    other = pick_off_bank(page, k, bank)
    if not other:
        return [f"demo search has no scan off the bank day {bank}"]
    reload_with(page, k, other)
    open_scans(page, k)
    page.fill(f'[name="q_scan_{k}"]', OTHER_QUERY)
    with page.expect_response(lambda r: "/vat/scans" in r.url):
        page.click(f'[data-desk="{k}"] [data-scan-search] button')
    page.wait_for_timeout(SWAP_MS)
    page.check(f'[data-desk="{k}"] input[name="scan_{k}"][value=""]')
    got = page.input_value(f'[name="day_{k}"]')
    return [] if got == bank else [f"day after a new search without a scan: {got} (want {bank})"]


def set_day_stays(page):
    """A clicked bank day or a typed day stays through another scan and a reload."""
    k = SCANNED
    page.click(f'.bench-entry[data-case="{k}"]')
    bank = page.get_attribute(f'[name="day_{k}"]', "data-bank-day")
    other = pick_off_bank(page, k, bank)
    if not other:
        return [f"demo search has no scan off the bank day {bank}"]
    page.click(f'[data-suggest-for="day-{k}"]:not([data-scan-src]) button')
    none = f'[data-desk="{k}"] input[name="scan_{k}"][value=""]'
    page.check(none)
    page.check(f'#found-scan-{k} input[value="{other}"]')
    wrong = []
    got = page.input_value(f'[name="day_{k}"]')
    if got != bank:
        wrong.append(f"clicked bank day after another scan: {got} (want {bank})")

    return wrong


def typed_day_stays(page):
    """A day typed with real keys stays through a reload and a dropped scan."""
    k = SCANNED
    page.click(f'.bench-entry[data-case="{k}"]')
    bank = page.get_attribute(f'[name="day_{k}"]', "data-bank-day")
    other = pick_off_bank(page, k, bank)
    if not other:
        return [f"demo search has no scan off the bank day {bank}"]
    page.focus(f'[name="day_{k}"]')
    page.keyboard.type(TYPED_KEYS)
    reload_with(page, k, other)
    open_scans(page, k)
    page.check(f'[data-desk="{k}"] input[name="scan_{k}"][value=""]')
    got = page.input_value(f'[name="day_{k}"]')
    return [] if got == TYPED_DAY else [f"typed day after a reload without a scan: {got} (want {TYPED_DAY})"]


def second_search(page):
    """A second search keeps the picked hit: the frame shows what is sent."""
    k = SCANNED
    page.click(f'.bench-entry[data-case="{k}"]')
    bank = page.get_attribute(f'[name="day_{k}"]', "data-bank-day")
    other = pick_off_bank(page, k, bank)
    if not other:
        return [f"demo search has no scan off the bank day {bank}"]
    page.fill(f'[name="q_scan_{k}"]', OTHER_QUERY)
    with page.expect_response(lambda r: "/vat/scans" in r.url):
        page.click(f'[data-desk="{k}"] [data-scan-search] button')
    page.wait_for_timeout(SWAP_MS)
    picked = page.eval_on_selector_all(f'[name="scan_{k}"]:checked', "els => els.map(e => e.value)")
    return [] if picked == [other] else [f"scan after a second search: {picked or 'none checked'} (want {other})"]


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


def second_search_reload(page):
    """The picked hit comes back by its id, not by the query now in the field."""
    k = SCANNED
    page.click(f'.bench-entry[data-case="{k}"]')
    bank = page.get_attribute(f'[name="day_{k}"]', "data-bank-day")
    other = pick_off_bank(page, k, bank)
    if not other:
        return [f"demo search has no scan off the bank day {bank}"]
    page.fill(f'[name="q_scan_{k}"]', OTHER_QUERY)
    page.wait_for_timeout(SAVE_MS)
    page.reload()
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(RESTORE_MS)
    picked = page.eval_on_selector_all(f'[name="scan_{k}"]:checked', "els => els.map(e => e.value)")
    if picked != [other]:
        return [f"scan after a second search and a reload: {picked or 'none checked'} (want {other})"]
    frame = page.get_attribute(f'[data-desk="{k}"] aside[data-for=create] [data-doc]', "src") or ""
    return [] if frame.endswith("/" + other) else [f"frame after a reload: {frame} (want {other})"]


def scan_gone(page):
    """A picked scan gone meanwhile: no scan is what the desk says and sends."""
    k = SCANNED
    page.click(f'.bench-entry[data-case="{k}"]')
    bank = page.get_attribute(f'[name="day_{k}"]', "data-bank-day")
    other = pick_off_bank(page, k, bank)
    if not other:
        return [f"demo search has no scan off the bank day {bank}"]
    page.wait_for_timeout(SAVE_MS)
    page.route("**/vat/scans*", lambda r: r.fulfill(status=200, content_type="text/html", body=""))
    page.reload()
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(RESTORE_MS)
    page.unroute("**/vat/scans*")
    picked = page.eval_on_selector_all(f'[name="scan_{k}"]:checked', "els => els.map(e => e.value)")
    day = page.input_value(f'[name="day_{k}"]')
    if picked != [""] or day != bank:
        return [f"scan gone after a reload: {picked or 'none checked'}, day {day} (want \"Ohne Beleg\", {bank})"]
    return []


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
        wrong = scan_day(page) + scan_reload(page) + bank_kept(page)
        discard(page)
        wrong += search_after_reload(page)
        discard(page)
        wrong += set_day_stays(page)
        discard(page)
        wrong += typed_day_stays(page)
        discard(page)
        wrong += second_search(page)
        discard(page)
        wrong += second_search_reload(page)
        discard(page)
        wrong += scan_gone(page)
        discard(page)
        wrong += unsorted(page)

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

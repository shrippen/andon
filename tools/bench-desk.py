#!/usr/bin/env python3
"""Works the VAT desk the way the owner does and checks what it says:
dropping a scan gives the expense its bank day back, an unsorted entry
also after a reload that put the draft back and a new search; a date
the user set (bank day clicked, day typed) stays through a reload and
another scan; a second search keeps the picked hit, also through a
reload and a save while its search runs; a picked scan gone meanwhile leaves no scan and the bank day, in
the action not shown too and without touching the one shown; a pick of
the action not shown stays out of the shown column; a failed search says why,
sends the bank day, and the next search brings pick and day back;
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
  scan after a save while its search ran: none checked (want 201)
  scan gone after a reload: none checked, day 2026-10-16 (want "Ohne Beleg", 2026-10-24)
  hidden pick gone: [''], day 2026-10-16 (want "Ohne Beleg", 2026-10-29)
  column after a reload: 'Rechnung 09/2026' (want 'Rechnung FE-2026-0917')
  search says it failed: none checked, day 2026-10-16 (want none, 2026-10-24)
  search says it failed, searched again: [''], day 2026-10-24 (want 201, 2026-10-16)
  search got a 502: no reason in the hits
  saved while failed: ['201'], day 2026-10-24 (want 201, 2026-10-16)
  review while failed: none checked, day 2026-10-16 (want none, 2026-10-24)
  both searches failed: none checked, day 2026-10-16 (want none, 2026-10-29)
  search lost the network: frame shows a receipt not picked
  take enabled while unsorted
  label after later: '' (want 'Noch unsortiert')

Exits 1 when one differs or the page threw.
"""

import sys

from playwright.sync_api import sync_playwright

LOGIN = ("mara@studio-weber.example.test", "demo-password-1")  # internal/services/seed DemoUser

# Demo debits (sources/demo_bank.go): one with a proposed scan, one unsorted.
SCANNED, UNSORTED = "demo-b-scan", "demo-p-ticket"
BOTH = "demo-bdemo-e11"  # create and link, each with its own scan radios
RECEIPT_DAY = "2020-01-02"  # far from any bank day
QUERY, OTHER_QUERY = "Rechnung", "Bahn"
TYPED_DAY, TYPED_KEYS = "2026-10-10", "10102026"  # same in day-first and month-first fields
FAILED_HITS = '<p class="error small">Paperless</p>'  # vat_found on a fetch error
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


def save_during_search(page):
    """A save while the pick's search still runs keeps the pick in the draft."""
    k = SCANNED
    page.click(f'.bench-entry[data-case="{k}"]')
    bank = page.get_attribute(f'[name="day_{k}"]', "data-bank-day")
    other = pick_off_bank(page, k, bank)
    if not other:
        return [f"demo search has no scan off the bank day {bank}"]
    page.wait_for_timeout(SAVE_MS)

    held = []
    page.route("**/vat/scans*", lambda r: held.append(r))
    page.reload()
    page.wait_for_selector(f'[name="note_{k}"]', state="attached")
    page.click(f'.bench-entry[data-case="{k}"]')
    page.focus(f'[name="note_{k}"]')
    page.keyboard.type("x")
    page.wait_for_timeout(SAVE_MS)
    for r in held:
        r.continue_()
    page.unroute("**/vat/scans*")
    page.wait_for_timeout(RESTORE_MS)

    page.reload()
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(RESTORE_MS)
    picked = page.eval_on_selector_all(f'[name="scan_{k}"]:checked', "els => els.map(e => e.value)")
    return [] if picked == [other] else [f"scan after a save while its search ran: {picked or 'none checked'} (want {other})"]


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


def desk_state(page, k):
    """What the desk shows and sends for the scan."""
    return (
        page.eval_on_selector_all(f'[name="scan_{k}"]:checked', "els => els.map(e => e.value)"),
        page.input_value(f'[name="day_{k}"]'),
    )


def failed_search(page, name, answer):
    """A search failing after a reload: the desk says why and sends the bank
    day; the next search brings the pick and its day back."""
    k = SCANNED
    page.click(f'.bench-entry[data-case="{k}"]')
    bank = page.get_attribute(f'[name="day_{k}"]', "data-bank-day")
    other = pick_off_bank(page, k, bank)
    if not other:
        return [f"demo search has no scan off the bank day {bank}"]
    day = page.get_attribute(f'#found-scan-{k} input[value="{other}"]', "data-day")
    page.fill(f'[name="q_scan_{k}"]', OTHER_QUERY)
    page.wait_for_timeout(SAVE_MS)

    page.route("**/vat/scans*", answer)
    page.reload()
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(RESTORE_MS)
    page.unroute("**/vat/scans*")

    wrong = []
    page.click(f'.bench-entry[data-case="{k}"]')
    picked, got = desk_state(page, k)
    if picked or got != bank:
        wrong.append(f"{name}: {picked or 'none checked'}, day {got} (want none, {bank})")
    if page.locator(f'#found-scan-{k} .error').count() != 1:
        wrong.append(f"{name}: no reason in the hits")
    if page.locator(f'[data-desk="{k}"] aside[data-for=create] [data-doc]').is_visible():
        wrong.append(f"{name}: frame shows a receipt not picked")

    open_scans(page, k)
    with page.expect_response(lambda r: "/vat/scans" in r.url):
        page.click(f'[data-desk="{k}"] [data-scan-search] button')
    page.wait_for_timeout(RESTORE_MS)
    picked, got = desk_state(page, k)
    if picked != [other] or got != day:
        wrong.append(f"{name}, searched again: {picked or 'none checked'}, day {got} (want {other}, {day})")
    return wrong


def fail_then_save(page, k):
    """Picks off the bank day, fails the search after a reload; (bank, scan, its day)."""
    page.click(f'.bench-entry[data-case="{k}"]')
    bank = page.get_attribute(f'[name="day_{k}"]', "data-bank-day")
    other = pick_off_bank(page, k, bank)
    if not other:
        return bank, None, None
    day = page.get_attribute(f'#found-scan-{k} input[value="{other}"]', "data-day")
    page.wait_for_timeout(SAVE_MS)
    page.route("**/vat/scans*", lambda r: r.fulfill(status=502, body=""))
    page.reload()
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(RESTORE_MS)
    page.click(f'.bench-entry[data-case="{k}"]')
    return bank, other, day


def healthy_reload(page):
    page.unroute("**/vat/scans*")
    page.wait_for_timeout(SAVE_MS)
    page.reload()
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(RESTORE_MS)


def failed_day_waits(page):
    """A save during a failed search keeps the pick's day, not the stand-in."""
    k = SCANNED
    bank, other, day = fail_then_save(page, k)
    if not other:
        return [f"demo search has no scan off the bank day {bank}"]
    page.click(f'[data-desk="{k}"] .callout')
    healthy_reload(page)
    picked, got = desk_state(page, k)
    return [] if picked == [other] and got == day else [f"saved while failed: {picked or 'none checked'}, day {got} (want {other}, {day})"]


def failed_review(page):
    """Another swap during a failed search leaves the stand-in; the pick's day comes with the pick."""
    k = SCANNED
    bank, other, day = fail_then_save(page, k)
    if not other:
        return [f"demo search has no scan off the bank day {bank}"]
    with page.expect_response(lambda r: "/vat/review" in r.url):
        page.click(f'[data-desk="{k}"] [hx-target="#one-{k}"]')
    page.wait_for_timeout(SWAP_MS)
    wrong = []
    picked, got = desk_state(page, k)
    if picked or got != bank:
        wrong.append(f"review while failed: {picked or 'none checked'}, day {got} (want none, {bank})")
    page.click(f'[data-desk="{k}"] .callout')
    healthy_reload(page)
    picked, got = desk_state(page, k)
    if picked != [other] or got != day:
        wrong.append(f"after a review while failed: {picked or 'none checked'}, day {got} (want {other}, {day})")
    return wrong


def failed_both(page):
    """Both searches failing: the other action's swap leaves the shown stand-in."""
    k = BOTH
    page.click(f'.bench-entry[data-case="{k}"]')
    page.click(f'[data-desk="{k}"] [data-set-act=link]')
    open_scans(page, k)
    page.fill(f'[name="q_xscan_{k}"]', QUERY)
    page.click(f'[data-desk="{k}"] aside[data-for=link] [data-scan-search] button')
    hits = f'#found-xscan-{k} input[type=radio]'
    page.wait_for_selector(hits, state="attached")
    page.locator(hits).last.check()
    page.click(f'[data-desk="{k}"] [data-set-act=create]')
    bank = page.get_attribute(f'[name="day_{k}"]', "data-bank-day")
    other = pick_off_bank(page, k, bank)
    if not other:
        return [f"demo search has no scan off the bank day {bank}"]
    page.wait_for_timeout(SAVE_MS)
    page.route("**/vat/scans*", lambda r: r.fulfill(status=200, content_type="text/html", body=FAILED_HITS))
    page.reload()
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(RESTORE_MS)
    page.unroute("**/vat/scans*")
    picked, got = desk_state(page, k)
    return [] if not picked and got == bank else [f"both searches failed: {picked or 'none checked'}, day {got} (want none, {bank})"]


def failed_no_scan(page):
    """"Ohne Beleg" chosen during a failed search stays, with the bank day."""
    k = SCANNED
    bank, other, _ = fail_then_save(page, k)
    if not other:
        return [f"demo search has no scan off the bank day {bank}"]
    open_scans(page, k)
    page.check(f'[data-desk="{k}"] input[name="scan_{k}"][value=""]')
    healthy_reload(page)
    picked, got = desk_state(page, k)
    return [] if picked == [""] and got == bank else [f"no scan while failed: {picked or 'none checked'}, day {got} (want \"Ohne Beleg\", {bank})"]


def failed_set_day(page):
    """The bank day clicked during a failed search is the user's: it stays."""
    k = SCANNED
    bank, other, _ = fail_then_save(page, k)
    if not other:
        return [f"demo search has no scan off the bank day {bank}"]
    page.click(f'[data-suggest-for="day-{k}"]:not([data-scan-src]) button')
    healthy_reload(page)
    picked, got = desk_state(page, k)
    return [] if picked == [other] and got == bank else [f"bank day set while failed: {picked or 'none checked'}, day {got} (want {other}, {bank})"]


def hidden_gone(page):
    """A pick gone from the action not shown gives its own date way."""
    k = BOTH
    page.click(f'.bench-entry[data-case="{k}"]')
    page.click(f'[data-desk="{k}"] [data-set-act=create]')
    bank = page.get_attribute(f'[name="day_{k}"]', "data-bank-day")
    other = pick_off_bank(page, k, bank)
    if not other:
        return [f"demo search has no scan off the bank day {bank}"]
    page.click(f'[data-desk="{k}"] [data-set-act=link]')
    page.wait_for_timeout(SAVE_MS)

    def answer(r):
        if "field=scan" in r.request.url:
            r.fulfill(status=200, content_type="text/html", body="")
            return
        r.continue_()

    page.route("**/vat/scans*", answer)
    page.reload()
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(RESTORE_MS)
    page.unroute("**/vat/scans*")
    page.click(f'.bench-entry[data-case="{k}"]')
    page.click(f'[data-desk="{k}"] [data-set-act=create]')
    picked, got = desk_state(page, k)
    if picked != [""] or got != bank:
        return [f"hidden pick gone: {picked or 'none checked'}, day {got} (want \"Ohne Beleg\", {bank})"]
    return []


def hidden_gone_shown(page):
    """A pick gone from the action not shown leaves the shown one alone."""
    k = BOTH
    page.click(f'.bench-entry[data-case="{k}"]')
    page.click(f'[data-desk="{k}"] [data-set-act=link]')
    open_scans(page, k)
    page.fill(f'[name="q_xscan_{k}"]', QUERY)
    page.click(f'[data-desk="{k}"] aside[data-for=link] [data-scan-search] button')
    hits = f'#found-xscan-{k} input[type=radio]'
    page.wait_for_selector(hits, state="attached")
    page.locator(hits).last.check()

    page.click(f'[data-desk="{k}"] [data-set-act=create]')
    bank = page.get_attribute(f'[name="day_{k}"]', "data-bank-day")
    other = pick_off_bank(page, k, bank)
    if not other:
        return [f"demo search has no scan off the bank day {bank}"]
    day = page.get_attribute(f'#found-scan-{k} input[value="{other}"]', "data-day")
    title = page.get_attribute(f'#found-scan-{k} input[value="{other}"]', "data-title")
    page.wait_for_timeout(SAVE_MS)

    def answer(r):
        if "field=xscan" in r.request.url:
            r.fulfill(status=200, content_type="text/html", body="")
            return
        r.continue_()

    page.route("**/vat/scans*", answer)
    page.reload()
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(RESTORE_MS)
    page.unroute("**/vat/scans*")
    picked, got = desk_state(page, k)
    column = page.inner_text(f'[data-desk="{k}"] div[data-for=create] [data-scan-text=title]')
    if picked != [other] or got != day or column != title:
        return [f"shown after a hidden pick gone: {picked}, day {got}, {column!r} (want {other}, {day}, {title!r})"]
    return []


def hidden_pick(page):
    """A pick of the action not shown coming back stays out of the shown column."""
    k = BOTH
    page.click(f'.bench-entry[data-case="{k}"]')
    page.click(f'[data-desk="{k}"] [data-set-act=link]')
    open_scans(page, k)
    page.fill(f'[name="q_xscan_{k}"]', QUERY)
    page.click(f'[data-desk="{k}"] aside[data-for=link] [data-scan-search] button')
    hits = f'#found-xscan-{k} input[type=radio]'
    page.wait_for_selector(hits, state="attached")
    linked = page.locator(hits).last
    taken = linked.get_attribute("value")
    linked.check()

    page.click(f'[data-desk="{k}"] [data-set-act=create]')
    bank = page.get_attribute(f'[name="day_{k}"]', "data-bank-day")
    other = pick_off_bank(page, k, bank)
    if not other or other == taken:
        return [f"demo search has no second scan: {other}, {taken}"]
    want = page.get_attribute(f'#found-scan-{k} input[value="{other}"]', "data-title")
    reload_with(page, k, other)
    page.wait_for_timeout(RESTORE_MS)
    got = page.inner_text(f'[data-desk="{k}"] div[data-for=create] [data-scan-text=title]')
    return [] if got == want else [f"column after a reload: {got!r} (want {want!r})"]


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
        wrong += save_during_search(page)
        discard(page)
        wrong += scan_gone(page)
        discard(page)
        wrong += failed_day_waits(page)
        discard(page)
        wrong += failed_review(page)
        discard(page)
        wrong += failed_both(page)
        discard(page)
        wrong += failed_no_scan(page)
        discard(page)
        wrong += failed_set_day(page)
        discard(page)
        wrong += hidden_gone(page)
        discard(page)
        wrong += hidden_gone_shown(page)
        discard(page)
        wrong += hidden_pick(page)
        discard(page)
        wrong += failed_search(page, "search says it failed", lambda r: r.fulfill(status=200, content_type="text/html", body=FAILED_HITS))
        discard(page)
        wrong += failed_search(page, "search got a 502", lambda r: r.fulfill(status=502, body=""))
        discard(page)
        wrong += failed_search(page, "search lost the network", lambda r: r.abort())
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

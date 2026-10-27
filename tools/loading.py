#!/usr/bin/env python3
"""Plays the waiting feedback (andon.js loadStart, Kante 1.47) against the
demo with slowed and failed answers:

  fast   a quick page change shows no bar
  page   slow page change: bar on top, clicked link runs, <main> busy
  esc    Esc cancels a slow page change, the page stays
  fail   a failed page change keeps the page, a toast offers a retry
  form   slow submit: the button is busy and disabled; no POST reaches
         the server (every POST is held, then dropped)
  slow   after 8 s a toast says "Dauert länger … 8 s"

Usage: start the demo (./start.sh demo, default http://localhost:8080), then
  <shrippen.github.io>/demo/tools/.venv/bin/python tools/loading.py [base-url]

Exits 1 when a case fails or the page threw.
"""

import sys
from playwright.sync_api import sync_playwright

LOGIN = ("mara@studio-weber.example.test", "demo-password-1")  # internal/services/seed DemoUser

# What the page shows while waiting.
STATE = """() => {
  const bar = document.querySelector('.page-load');
  const main = document.querySelector('main');
  return {
    bar: bar ? getComputedStyle(bar).opacity : '',
    pending: document.querySelectorAll('.is-pending').length,
    busy: main ? main.getAttribute('aria-busy') || '' : '',
    toast: [...document.querySelectorAll('#toast .toast')].map(t => t.textContent.trim()),
  };
}"""

LINK = ".app-links a[href='/hints']"


def login(page, base):
    page.goto(base + "/login")
    page.fill("#email", LOGIN[0])
    page.fill("#password", LOGIN[1])
    page.click("form[action='/login'] button[type=submit]")
    page.wait_for_load_state("networkidle")


class Hold:
    """Holds matching requests until release(); the browser waits meanwhile.
    Route handlers run on Playwright's loop, so they must not sleep."""

    def __init__(self, page, pattern, method=None):
        self.page, self.pattern, self.method, self.routes = page, pattern, method, []
        page.route(pattern, self.take)

    def take(self, route):
        if self.method and route.request.method != self.method:
            route.continue_()
            return
        self.routes.append(route)

    def release(self, then="continue"):
        for route in self.routes:
            route.abort() if then == "abort" else route.continue_()
        self.routes = []
        self.page.unroute(self.pattern, self.take)


def check(browser, base):
    page = browser.new_page(viewport={"width": 1440, "height": 960})
    errors, fails = [], []
    page.on("pageerror", lambda e: errors.append(str(e)))
    login(page, base)

    def want(case, ok, got):
        print(f"{case:5} {'ok' if ok else 'FAIL'}  {got}")
        if not ok:
            fails.append(case)

    # fast: sampled mid-way, the bar is still hidden (150 ms delay).
    page.goto(base + "/")
    page.click(LINK, no_wait_after=True)
    page.wait_for_timeout(60)
    got = page.evaluate(STATE)
    want("fast", got["bar"] in ("0", ""), got)
    page.wait_for_load_state("networkidle")

    # page: a held page change shows the states after 150 ms.
    page.goto(base + "/")
    hold = Hold(page, "**/hints")
    page.click(LINK, no_wait_after=True)
    page.wait_for_timeout(600)
    got = page.evaluate(STATE)
    want("page", got["bar"] == "1" and got["pending"] == 1 and got["busy"] == "true", got)
    hold.release()
    page.wait_for_url("**/hints")
    page.wait_for_load_state("networkidle")
    got = page.evaluate(STATE)
    want("done", got["bar"] == "0" and got["pending"] == 0 and got["busy"] == "", got)

    # esc: Esc cancels, the start page stays.
    page.goto(base + "/")
    hold = Hold(page, "**/hints")
    page.click(LINK, no_wait_after=True)
    page.wait_for_timeout(400)
    page.keyboard.press("Escape")
    page.wait_for_timeout(800)
    got = page.evaluate(STATE)
    want("esc", not page.url.endswith("/hints") and got["pending"] == 0 and got["busy"] == "", got)
    hold.release("abort")

    # fail: a dropped page change keeps the page and offers a retry.
    page.goto(base + "/")
    hold = Hold(page, "**/hints")
    page.click(LINK, no_wait_after=True)
    page.wait_for_timeout(300)
    hold.release("abort")
    page.wait_for_timeout(800)
    got = page.evaluate(STATE)
    retry = page.locator("#toast .toast[data-kind=error] .toast-action")
    want("fail", not page.url.endswith("/hints") and retry.count() == 1 and got["busy"] == "", got)
    # again: a retry that fails too says so again (it counts as the user's).
    if retry.count():
        hold = Hold(page, "**/hints")
        retry.click()
        page.wait_for_timeout(300)
        hold.release("abort")
        page.wait_for_timeout(800)
        retry = page.locator("#toast .toast[data-kind=error] .toast-action")
        want("again", retry.count() == 1, f"retry={retry.count()}")
    if retry.count():
        retry.click()
        page.wait_for_url("**/hints")
        want("retry", page.url.endswith("/hints"), page.url)

    # form: a held submit marks its button; the POST never arrives.
    page.goto(base + "/hints")
    hold = Hold(page, "**/*", "POST")
    button = page.locator("main form[method=post] button[type=submit]:visible").first
    button.click(no_wait_after=True)
    page.wait_for_timeout(500)
    busy = button.evaluate("b => b.classList.contains('is-busy') && b.disabled")
    want("form", busy, f"busy={busy}")
    hold.release("abort")
    page.wait_for_timeout(800)
    calm = button.evaluate("b => !b.classList.contains('is-busy') && !b.disabled")
    want("after", calm, f"calm={calm}")
    # postfail: a dropped form may have arrived: an error, but no retry
    # that could write twice.
    toast = page.locator("#toast .toast[data-kind=error]").last
    action = page.locator("#toast .toast[data-kind=error] .toast-action").count()
    want("postfail", toast.count() == 1 and action == 0, f"toast={toast.count()} retry={action}")
    page.evaluate("document.querySelectorAll('#toast .toast').forEach(t => t.remove())")

    # escpost: Esc leaves a sent page form (boosted POST) alone; the
    # server writes it anyway.
    page.goto(base + "/hints")
    hold = Hold(page, "**/*", "POST")
    button = page.locator("main form[method=post]:not([hx-post]) button[type=submit]:visible").first
    button.click(no_wait_after=True)
    page.wait_for_timeout(400)
    page.keyboard.press("Escape")
    page.wait_for_timeout(400)
    busy = button.evaluate("b => b.classList.contains('is-busy')")
    want("escpost", busy, f"busy={busy}")
    hold.release("abort")
    page.wait_for_timeout(800)

    # partretry: a dropped hint detail (toggle from: details, once) loads
    # again from the toast's retry.
    page.goto(base + "/hints")
    hold = Hold(page, "**/hints/*/detail")
    page.locator("main details:has([hx-get$='/detail']) > summary").first.click()
    page.wait_for_timeout(300)
    hold.release("abort")
    page.wait_for_timeout(500)
    retry = page.locator("#toast .toast[data-kind=error] .toast-action")
    if retry.count():
        retry.first.click()
        page.wait_for_timeout(1200)
    loaded = page.locator("main details[open] [hx-get$='/detail'] .loading").count() == 0
    want("partretry", retry.count() == 0 and loaded, f"loaded={loaded}")

    # slow: after 8 s the page change says it takes longer.
    page.goto(base + "/")
    hold = Hold(page, "**/hints")
    page.click(LINK, no_wait_after=True)
    page.wait_for_timeout(8600)
    got = page.evaluate(STATE)
    want("slow", any(t.endswith(" 8 s") for t in got["toast"]), got["toast"])
    hold.release()
    page.wait_for_url("**/hints")

    want("errors", not errors, errors)
    page.close()
    return not fails


def main():
    base = sys.argv[1].rstrip("/") if len(sys.argv) > 1 else "http://localhost:8080"
    with sync_playwright() as p:
        browser = p.chromium.launch()
        ok = check(browser, base)
        browser.close()
    sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()

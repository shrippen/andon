#!/usr/bin/env python3
"""Opens a board in edit mode by a full page load, as a reload does, and
checks that every tile is ready to edit without a script error.

Usage: start the demo (./start.sh demo, default http://localhost:8080), then
  <shrippen.github.io>/demo/tools/.venv/bin/python tools/edit-reload.py [base-url]

A soft page change (hx-boost) ran the editor after DOMContentLoaded; a
reload runs it before, and Kante's live API was not there yet:

  chromium  slots 20  editable 5  errors: kante.edit is not a function

Exits 1 when a tile is not editable or the page threw.
"""

import sys

from playwright.sync_api import sync_playwright

LOGIN = ("mara@studio-weber.example.test", "demo-password-1")  # internal/services/seed DemoUser

# What the page holds once the editor ran.
STATE = """() => ({
  slots: document.querySelectorAll('.board[data-mode] .tile-slot[data-placement]').length,
  editable: document.querySelectorAll('.board[data-mode] .tile-slot[data-editable]').length,
})"""


def check(browser, base):
    page = browser.new_page(viewport={"width": 1440, "height": 960})
    errors = []
    page.on("pageerror", lambda e: errors.append(str(e)))

    page.goto(base + "/login")
    page.fill("#email", LOGIN[0])
    page.fill("#password", LOGIN[1])
    page.click("form[action='/login'] button[type=submit]")
    page.wait_for_load_state("networkidle")

    # Full loads only: the edit URL typed in, then reloaded.
    board = page.evaluate("document.querySelector('.board[data-board]').getAttribute('data-board')")
    page.goto(f"{base}/boards/{board}?edit")
    page.reload()
    page.wait_for_load_state("networkidle")
    page.wait_for_timeout(1000)

    state = page.evaluate(STATE)
    page.close()
    return state, errors


def main():
    base = sys.argv[1] if len(sys.argv) > 1 else "http://localhost:8080"
    failed = False
    with sync_playwright() as pw:
        for kind in (pw.chromium, pw.firefox):
            browser = kind.launch()
            state, errors = check(browser, base)
            browser.close()

            ok = state["slots"] > 0 and state["editable"] == state["slots"] and not errors
            failed = failed or not ok
            print(f"{kind.name:9} slots {state['slots']}  editable {state['editable']}  errors: {'; '.join(errors) or '-'}")
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()

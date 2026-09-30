/* Runs before vendor/kante/shrippen.js. That script sets <html lang> from
 * its own stored choice ("shrippen-lang", English by default) and would
 * undo Andon's language, which the server already put on <html>. Andon has
 * its own i18n, so hand the script Andon's language as its stored choice.
 * The locale also goes to data-locale; andon.js restores it if storage is
 * blocked. */
(function () {
  "use strict";

  var html = document.documentElement;
  html.setAttribute("data-locale", html.lang);
  try {
    localStorage.setItem("shrippen-lang", html.lang);
  } catch (e) {
    /* storage blocked: andon.js restores the language */
  }
})();

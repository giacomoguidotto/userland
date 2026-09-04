// userland:quotas-ui (start)
// Restyles aiusage's Quotas page: two provider sections with their official
// marks, cards titled by account name alone.
//
// It does not edit the app's own DOM, which Svelte owns and rewrites on every
// refresh. Instead it hides the stock grid and renders clones of its cards, so
// the styling stays identical to the app's and a re-render just re-clones.
// The card title carries the "<provider>:<account>" id the patched server emits.
(function () {
  "use strict";

  var ICONS = {
    claude: '<svg viewBox="0 0 24 24" aria-hidden="true"><path fill="#D97757" d="m4.7144 15.9555 4.7174-2.6471.079-.2307-.079-.1275h-.2307l-.7893-.0486-2.6956-.0729-2.3375-.0971-2.2646-.1214-.5707-.1215-.5343-.7042.0546-.3522.4797-.3218.686.0608 1.5179.1032 2.2767.1578 1.6514.0972 2.4468.255h.3886l.0546-.1579-.1336-.0971-.1032-.0972L6.973 9.8356l-2.55-1.6879-1.3356-.9714-.7225-.4918-.3643-.4614-.1578-1.0078.6557-.7225.8803.0607.2246.0607.8925.686 1.9064 1.4754 2.4893 1.8336.3643.3035.1457-.1032.0182-.0728-.164-.2733-1.3539-2.4467-1.445-2.4893-.6435-1.032-.17-.6194c-.0607-.255-.1032-.4674-.1032-.7285L6.287.1335 6.6997 0l.9957.1336.419.3642.6192 1.4147 1.0018 2.2282 1.5543 3.0296.4553.8985.2429.8318.091.255h.1579v-.1457l.1275-1.706.2368-2.0947.2307-2.6957.0789-.7589.3764-.9107.7468-.4918.5828.2793.4797.686-.0668.4433-.2853 1.8517-.5586 2.9021-.3643 1.9429h.2125l.2429-.2429.9835-1.3053 1.6514-2.0643.7286-.8196.85-.9046.5464-.4311h1.0321l.759 1.1293-.34 1.1657-1.0625 1.3478-.8804 1.1414-1.2628 1.7-.7893 1.36.0729.1093.1882-.0183 2.8535-.607 1.5421-.2794 1.8396-.3157.8318.3886.091.3946-.3278.8075-1.967.4857-2.3072.4614-3.4364.8136-.0425.0304.0486.0607 1.5482.1457.6618.0364h1.621l3.0175.2247.7892.522.4736.6376-.079.4857-1.2142.6193-1.6393-.3886-3.825-.9107-1.3113-.3279h-.1822v.1093l1.0929 1.0686 2.0035 1.8092 2.5075 2.3314.1275.5768-.3218.4554-.34-.0486-2.2039-1.6575-.85-.7468-1.9246-1.621h-.1275v.17l.4432.6496 2.3436 3.5214.1214 1.0807-.17.3521-.6071.2125-.6679-.1214-1.3721-1.9246L14.38 17.959l-1.1414-1.9428-.1397.079-.674 7.2552-.3156.3703-.7286.2793-.6071-.4614-.3218-.7468.3218-1.4753.3886-1.9246.3157-1.53.2853-1.9004.17-.6314-.0121-.0425-.1397.0182-1.4328 1.9672-2.1796 2.9446-1.7243 1.8456-.4128.164-.7164-.3704.0667-.6618.4008-.5889 2.386-3.0357 1.4389-1.882.929-1.0868-.0062-.1579h-.0546l-6.3385 4.1164-1.1293.1457-.4857-.4554.0608-.7467.2307-.2429 1.9064-1.3114Z"/></svg>',
    codex: '<svg viewBox="0 0 2406 2406" aria-hidden="true"><g fill="currentColor">' +
      '<path id="ul-oai-knot" d="M1107.3 299.1c-197.999 0-373.9 127.3-435.2 315.3L650 743.5v427.9c0 21.4 11 40.4 29.4 51.4l344.5 198.515V833.3h.1v-27.9L1372.7 604c33.715-19.52 70.44-32.857 108.47-39.828L1447.6 450.3C1361 353.5 1237.1 298.5 1107.3 299.1zm0 117.5-.6.6c79.699 0 156.3 27.5 217.6 78.4-2.5 1.2-7.4 4.3-11 6.1L952.8 709.3c-18.4 10.4-29.4 30-29.4 51.4V1248l-155.1-89.4V755.8c-.1-187.099 151.601-338.9 339-339.2z"/>' +
      '<use href="#ul-oai-knot" transform="rotate(60 1203 1203)"/>' +
      '<use href="#ul-oai-knot" transform="rotate(120 1203 1203)"/>' +
      '<use href="#ul-oai-knot" transform="rotate(180 1203 1203)"/>' +
      '<use href="#ul-oai-knot" transform="rotate(240 1203 1203)"/>' +
      '<use href="#ul-oai-knot" transform="rotate(300 1203 1203)"/>' +
      '</g></svg>'
  };
  var TITLES = { claude: "Claude Code", codex: "Codex" };
  var ORDER = ["claude", "codex"];

  var STYLE = [
    ".ul-quotas .ul-section + .ul-section { margin-top: 2rem; }",
    ".ul-quotas .ul-section-head { display: flex; align-items: center; gap: .55rem; margin: 0 0 .85rem; }",
    ".ul-quotas .ul-section-head svg { width: 1.15rem; height: 1.15rem; flex: none; }",
    ".ul-quotas .ul-section-head h2 { font-size: .95rem; font-weight: 600; margin: 0; letter-spacing: .01em; }",
    "[data-ul-quotas-active] .quota-grid:not(.ul-grid) { display: none !important; }",
    "[data-ul-quotas-active] .ul-hidden { display: none !important; }"
  ].join("\n");

  function ensureStyle() {
    if (document.getElementById("ul-quotas-style")) return;
    var el = document.createElement("style");
    el.id = "ul-quotas-style";
    el.textContent = STYLE;
    document.head.appendChild(el);
  }

  function render(grid) {
    // Only the app's own cards: our clones carry the same classes, so an
    // unscoped query would re-clone them on every re-render.
    var cards = [];
    var candidates = document.querySelectorAll(".quota-card, .inactive-card");
    for (var q = 0; q < candidates.length; q++) {
      if (!candidates[q].closest("#ul-quotas")) cards.push(candidates[q]);
    }
    if (!cards.length) return;

    // The stock page files credential-less tools under its own heading. Those
    // cards move into their provider section so nothing disappears silently.
    var inactive = document.querySelector(".inactive-list");
    if (inactive) {
      inactive.classList.add("ul-hidden");
      var heading = inactive.previousElementSibling;
      if (heading && heading.classList.contains("section-title")) heading.classList.add("ul-hidden");
    }

    var groups = {};
    for (var i = 0; i < cards.length; i++) {
      var card = cards[i];
      var nameEl = card.querySelector(".tool-name");
      if (!nameEl) continue;
      var raw = nameEl.textContent.trim();
      var split = raw.indexOf(":");
      var provider = split > 0 ? raw.slice(0, split) : "";
      var account = split > 0 ? raw.slice(split + 1) : raw;
      if (!ICONS[provider]) provider = "claude";
      var clone = card.cloneNode(true);
      var cloneName = clone.querySelector(".tool-name");
      if (cloneName) cloneName.textContent = account;
      (groups[provider] = groups[provider] || []).push(clone);
    }

    var host = document.getElementById("ul-quotas");
    if (!host) {
      host = document.createElement("div");
      host.id = "ul-quotas";
      host.className = "ul-quotas";
      grid.parentNode.insertBefore(host, grid.nextSibling);
    }
    host.textContent = "";

    for (var g = 0; g < ORDER.length; g++) {
      var key = ORDER[g];
      if (!groups[key] || !groups[key].length) continue;
      var section = document.createElement("section");
      section.className = "ul-section";
      var head = document.createElement("div");
      head.className = "ul-section-head";
      head.innerHTML = ICONS[key] + "<h2>" + TITLES[key] + "</h2>";
      var row = document.createElement("div");
      row.className = grid.className + " ul-grid";
      for (var c = 0; c < groups[key].length; c++) row.appendChild(groups[key][c]);
      section.appendChild(head);
      section.appendChild(row);
      host.appendChild(section);
    }
    document.body.setAttribute("data-ul-quotas-active", "");
  }

  var pending = null;
  function sync() {
    var grid = document.querySelector(".quota-grid");
    if (!grid) {
      var stale = document.getElementById("ul-quotas");
      if (stale) stale.remove();
      document.body.removeAttribute("data-ul-quotas-active");
      return;
    }
    ensureStyle();
    render(grid);
  }
  function schedule() {
    if (pending) return;
    pending = requestAnimationFrame(function () { pending = null; sync(); });
  }

  function start() {
    schedule();
    new MutationObserver(function (records) {
      for (var i = 0; i < records.length; i++) {
        var target = records[i].target;
        if (target && target.closest && target.closest("#ul-quotas")) continue;
        schedule();
        return;
      }
    }).observe(document.body, { childList: true, subtree: true, characterData: true });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", start);
  } else {
    start();
  }
})();
// userland:quotas-ui (end)

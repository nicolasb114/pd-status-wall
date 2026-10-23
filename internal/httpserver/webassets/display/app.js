(function () {
  "use strict";

  var POLL_MS = 30000;
  var STATUS_LABELS = {
    operational: "Operational",
    warning: "Warning",
    critical: "Critical",
    maintenance: "Maintenance",
    disabled: "Disabled",
    impacted: "Impacted",
  };

  // Keyed by tree position (path of ids from the root), not by service id
  // alone: the same underlying service can legitimately appear under
  // several different parents (a shared dependency), and each occurrence
  // should open/close independently rather than all following whichever
  // one was clicked.
  var openState = Object.create(null);

  function el(sel, root) { return (root || document).querySelector(sel); }

  function renderNode(node, tpl, parentPath) {
    var path = (parentPath ? parentPath + "/" : "") + node.id;
    var frag = tpl.content.cloneNode(true);
    var nodeEl = frag.querySelector(".node");
    var row = frag.querySelector(".node-row");
    var dot = frag.querySelector(".status-dot");
    var name = frag.querySelector(".node-name");
    var label = frag.querySelector(".status-label");
    var childrenEl = frag.querySelector(".node-children");

    nodeEl.dataset.id = node.id;
    nodeEl.dataset.kind = node.kind || "";
    name.textContent = node.name;
    dot.dataset.status = node.status;
    label.textContent = STATUS_LABELS[node.status] || node.status;

    var hasChildren = node.children && node.children.length > 0;
    if (!hasChildren) {
      nodeEl.classList.add("leaf");
    } else {
      // A group's members are the point of the group, so it starts open;
      // supporting services underneath a service start collapsed.
      var defaultOpen = node.kind === "group";
      var isOpen = openState[path] === undefined ? defaultOpen : !!openState[path];
      if (isOpen) nodeEl.classList.add("open");
      row.addEventListener("click", function () {
        var willOpen = !nodeEl.classList.contains("open");
        nodeEl.classList.toggle("open", willOpen);
        openState[path] = willOpen;
      });
      node.children.forEach(function (child) {
        childrenEl.appendChild(renderNode(child, tpl, path));
      });
    }
    return nodeEl;
  }

  // Scheme backgrounds, kept in step with the tokens in style.css. Used
  // only to judge whether a configured text colour is readable on them.
  var SCHEME_BG = { light: "#f4f5f7", dark: "#101114" };
  var SCHEME_TEXT = { light: "#1a1a1a", dark: "#eceef2" };

  function relativeLuminance(hex) {
    var m = /^#?([0-9a-f]{6})$/i.exec(String(hex).trim());
    if (!m) return null;
    var n = parseInt(m[1], 16);
    var channels = [(n >> 16) & 255, (n >> 8) & 255, n & 255].map(function (c) {
      c = c / 255;
      return c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
    });
    return 0.2126 * channels[0] + 0.7152 * channels[1] + 0.0722 * channels[2];
  }

  function contrastRatio(a, b) {
    var la = relativeLuminance(a);
    var lb = relativeLuminance(b);
    if (la === null || lb === null) return null;
    return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05);
  }

  function resolveScheme(configured) {
    if (configured === "dark" || configured === "light") return configured;
    // "auto": follow the screen's own setting, which is what a wall driven
    // by a kiosk box or a TV's browser will have been set to.
    var prefersDark = window.matchMedia &&
      window.matchMedia("(prefers-color-scheme: dark)").matches;
    return prefersDark ? "dark" : "light";
  }

  function applyBranding(state) {
    var root = document.documentElement;
    root.dataset.theme = state.theme_mode === "grayscale" ? "grayscale" : "color";

    var scheme = resolveScheme(state.color_scheme);
    root.dataset.scheme = scheme;
    root.style.setProperty("--primary", state.primary_color || "#e30000");

    // A text colour picked against a light background becomes unreadable
    // on a dark one (and vice versa). This is an unattended wall, so fall
    // back to the scheme's own text colour rather than render something
    // nobody can read. 4.5:1 is the WCAG AA threshold for body text.
    var chosen = state.text_color;
    var ratio = chosen ? contrastRatio(chosen, SCHEME_BG[scheme]) : null;
    root.style.setProperty(
      "--text",
      ratio !== null && ratio >= 4.5 ? chosen : SCHEME_TEXT[scheme]
    );

    var cards = el("#cards");
    if (cards) cards.dataset.columns = state.columns > 0 ? String(state.columns) : "auto";

    var logoImg = el("#logo-img");
    var bannerImg = el("#banner-img");
    if (state.logo_url) {
      logoImg.src = state.logo_url;
      logoImg.hidden = false;
    } else {
      logoImg.hidden = true;
    }
    if (state.banner_url) {
      bannerImg.src = state.banner_url;
      bannerImg.dataset.fit = state.banner_fit_mode || "contain";
      bannerImg.hidden = false;
    } else {
      bannerImg.hidden = true;
    }
  }

  function applyOverall(state) {
    var overallEl = el("#overall");
    var icon = el("#overall-icon");
    var title = el("#page-title");
    var sub = el("#overall-message");

    overallEl.dataset.align = state.overall_align === "center" ? "center" : "left";
    overallEl.dataset.size = state.overall_size || "medium";
    overallEl.dataset.font = state.font_family || "system";

    if (!state.overall) {
      // The poller hasn't completed its first cycle yet (a brief race
      // right at cold start) - keep whatever placeholder/last-good text
      // is already on screen rather than flashing a blank message.
      return;
    }

    overallEl.dataset.status = state.overall;
    icon.textContent = state.overall === "operational" ? "✓" : "!";
    // The status page's own name is the prominent headline; the generic
    // smoothly-running/impacted message is the small line underneath.
    title.textContent = state.status_page_name || "Status";
    sub.textContent = state.overall_message;
  }

  function applyButtons(state) {
    var container = el("#buttons");
    container.innerHTML = "";
    (state.buttons || []).forEach(function (btn) {
      var a = document.createElement("a");
      a.className = "wall-button";
      a.href = btn.url;
      a.textContent = btn.label;
      a.target = "_blank";
      a.rel = "noopener noreferrer";
      container.appendChild(a);
    });
  }

  function applyCards(state) {
    var container = el("#cards");
    var tpl = el("#tpl-node");
    container.innerHTML = "";
    (state.services || []).forEach(function (node) {
      container.appendChild(renderNode(node, tpl, ""));
    });
  }

  function formatUpdatedAt(iso) {
    if (!iso) return "";
    try {
      var d = new Date(iso);
      if (d.getFullYear() < 1971) return ""; // zero-value time - no successful poll yet
      return "Last updated " + d.toLocaleTimeString();
    } catch (e) {
      return "";
    }
  }

  function refresh() {
    fetch("/api/state", { cache: "no-store" })
      .then(function (res) { return res.json(); })
      .then(function (state) {
        applyBranding(state);
        applyOverall(state);
        applyButtons(state);
        applyCards(state);
        el("#updated-at").textContent = formatUpdatedAt(state.generated_at);
      })
      .catch(function (err) {
        // Keep whatever was already rendered on screen - never blank out
        // the wall because of a transient client-side fetch error.
        console.error("failed to refresh state", err);
      });
  }

  refresh();
  setInterval(refresh, POLL_MS);
})();

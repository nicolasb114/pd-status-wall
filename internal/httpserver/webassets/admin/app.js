(function () {
  "use strict";

  function $(sel, root) { return (root || document).querySelector(sel); }
  function $all(sel, root) { return Array.prototype.slice.call((root || document).querySelectorAll(sel)); }

  function api(path, opts) {
    opts = opts || {};
    opts.headers = Object.assign({ "Content-Type": "application/json" }, opts.headers || {});
    return fetch(path, opts).then(function (res) {
      return res.json().catch(function () { return {}; }).then(function (body) {
        if (!res.ok) {
          var err = new Error(body.error || ("Request failed (" + res.status + ")"));
          err.status = res.status;
          throw err;
        }
        return body;
      });
    });
  }

  function flash(indicatorEl, ok, message) {
    indicatorEl.textContent = message || (ok ? "Saved" : "Failed");
    indicatorEl.className = "save-indicator show " + (ok ? "ok" : "err");
    setTimeout(function () {
      indicatorEl.classList.remove("show");
    }, 2500);
  }

  // --- Boot: decide login vs app screen ---

  function boot() {
    api("/admin/api/config")
      .then(showApp)
      .catch(function () { showLogin(); });
  }

  function showLogin() {
    $("#login-screen").hidden = false;
    $("#app-screen").hidden = true;
  }

  function showApp(cfg) {
    $("#login-screen").hidden = true;
    $("#app-screen").hidden = false;
    populateForms(cfg);
    setupNav();
  }

  $("#login-form").addEventListener("submit", function (e) {
    e.preventDefault();
    var errorEl = $("#login-error");
    errorEl.hidden = true;
    api("/admin/login", {
      method: "POST",
      body: JSON.stringify({
        username: $("#login-username").value,
        password: $("#login-password").value,
      }),
    })
      .then(boot)
      .catch(function (err) {
        errorEl.textContent = err.message || "Invalid username or password";
        errorEl.hidden = false;
      });
  });

  $("#logout-btn").addEventListener("click", function () {
    api("/admin/logout", { method: "POST" }).finally(boot);
  });

  // --- Sidebar navigation ---

  function setupNav() {
    $all(".nav-item").forEach(function (btn) {
      btn.addEventListener("click", function () {
        $all(".nav-item").forEach(function (b) { b.classList.remove("active"); });
        $all(".tab").forEach(function (t) { t.classList.remove("active"); });
        btn.classList.add("active");
        $("#tab-" + btn.dataset.tab).classList.add("active");
      });
    });
    if (!$(".nav-item.active")) {
      $(".nav-item").classList.add("active");
      $(".tab").classList.add("active");
    }
  }

  // --- Populate all forms from /admin/api/config ---

  function populateForms(cfg) {
    $("#pd-region").value = cfg.pd_region || "us";
    $("#pd-poll-interval").value = cfg.poll_interval_seconds || 45;
    $("#pd-api-key").placeholder = cfg.pd_api_key_set
      ? "Current key: " + cfg.pd_api_key_masked
      : "No API key set yet";
    $("#pd-api-key-hint").textContent = cfg.pd_api_key_set
      ? "Leave blank to keep the currently saved key."
      : "Paste a read-only PagerDuty API token.";

    $("#banner-fit-mode").value = cfg.banner_fit_mode || "contain";
    $("#theme-mode").value = cfg.theme_mode || "color";
    $("#color-scheme").value = cfg.color_scheme || "light";
    $("#columns").value = String(cfg.columns || 0);
    $("#primary-color").value = cfg.primary_color || "#e30000";
    $("#text-color").value = cfg.text_color || "#1a1a1a";
    $("#overall-align").value = cfg.overall_align || "left";
    $("#overall-size").value = cfg.overall_size || "medium";
    $("#font-family").value = cfg.font_family || "system";
    updatePreview();

    renderButtons(cfg.buttons || []);
    $("#button-position").value = cfg.button_position || "below";
    $("#button-align").value = cfg.button_align || "left";
    $("#button-style").value = cfg.button_style || "solid";
    $("#button-size").value = cfg.button_size || "medium";
    updateButtonsPreview();
    renderCidrList(cfg.cidr_allowlist || []);

    if (cfg.pd_api_key_set) {
      loadStatusPages(cfg.status_page_id);
    }

    var pollerStatus = $("#poller-status");
    if (cfg.last_poll_ok) {
      pollerStatus.textContent = "Last successful poll: " + (cfg.last_poll_at || "unknown");
      pollerStatus.classList.remove("error");
    } else if (cfg.last_error) {
      pollerStatus.textContent = "Poller error: " + cfg.last_error +
        (cfg.last_poll_at ? " (as of " + cfg.last_poll_at + ")" : "");
    } else {
      pollerStatus.textContent = "Waiting for the first poll...";
    }

    window.__savedServiceOrder = cfg.service_order || [];
    window.__savedStatusPageId = cfg.status_page_id || "";
    window.__savedGroups = cfg.service_groups || [];
    $("#show-sub-services").checked = !!cfg.show_sub_services;
  }

  // --- PagerDuty connection ---

  $("#toggle-key-visibility").addEventListener("click", function () {
    var input = $("#pd-api-key");
    var show = input.type === "password";
    input.type = show ? "text" : "password";
    this.textContent = show ? "Hide" : "Show";
  });

  $("#form-pagerduty").addEventListener("submit", function (e) {
    e.preventDefault();
    var indicator = $("#save-pagerduty");
    api("/admin/api/pagerduty", {
      method: "POST",
      body: JSON.stringify({
        pd_api_key: $("#pd-api-key").value,
        pd_region: $("#pd-region").value,
        poll_interval_seconds: parseInt($("#pd-poll-interval").value, 10) || 45,
      }),
    })
      .then(function () {
        flash(indicator, true);
        $("#pd-api-key").value = "";
        loadStatusPages();
      })
      .catch(function (err) { flash(indicator, false, err.message); });
  });

  // --- Status pages + service ordering ---

  function loadStatusPages(preselectId) {
    var select = $("#status-page-select");
    select.innerHTML = "<option value=\"\">Loading...</option>";
    api("/admin/api/status-pages")
      .then(function (data) {
        var pages = data.status_pages || [];
        select.innerHTML = "";
        if (pages.length === 0) {
          select.innerHTML = "<option value=\"\">No status pages found</option>";
          return;
        }
        pages.forEach(function (p) {
          var opt = document.createElement("option");
          opt.value = p.id;
          opt.textContent = p.name;
          select.appendChild(opt);
        });
        select.value = preselectId || window.__savedStatusPageId || pages[0].id;
        loadServicesForSelectedPage();
      })
      .catch(function (err) {
        select.innerHTML = "<option value=\"\">" + (err.message || "Failed to load") + "</option>";
      });
  }

  $("#refresh-status-pages").addEventListener("click", function () { loadStatusPages(); });
  $("#status-page-select").addEventListener("change", loadServicesForSelectedPage);

  function loadServicesForSelectedPage() {
    var pageId = $("#status-page-select").value;
    var list = $("#service-order-list");
    if (!pageId) { list.innerHTML = ""; return; }
    list.innerHTML = "<p class=\"muted\">Loading services...</p>";
    api("/admin/api/status-pages/services?status_page_id=" + encodeURIComponent(pageId))
      .then(function (data) {
        var services = data.services || [];
        var order = (pageId === window.__savedStatusPageId) ? window.__savedServiceOrder : [];
        services.sort(function (a, b) {
          var ia = order.indexOf(a.business_service.id);
          var ib = order.indexOf(b.business_service.id);
          if (ia === -1) ia = 999;
          if (ib === -1) ib = 999;
          return ia - ib;
        });
        window.__availableServices = services.map(function (s) {
          return { id: s.business_service.id, name: s.name };
        });
        renderServiceOrderList(services);
        renderGroups(pageId === window.__savedStatusPageId ? window.__savedGroups : []);
      })
      .catch(function (err) {
        list.innerHTML = "<p class=\"error\">" + (err.message || "Failed to load services") + "</p>";
      });
  }

  function renderServiceOrderList(services) {
    var list = $("#service-order-list");
    list.innerHTML = "";
    if (services.length === 0) {
      list.innerHTML = "<p class=\"muted\">This status page has no business services configured.</p>";
      return;
    }
    services.forEach(function (svc) {
      var row = document.createElement("div");
      row.className = "reorder-row";
      row.dataset.id = svc.business_service.id;
      row.innerHTML =
        '<span class="reorder-name"></span>' +
        '<div class="reorder-controls">' +
        '<button type="button" class="btn btn-ghost btn-icon" data-move="up">&uarr;</button>' +
        '<button type="button" class="btn btn-ghost btn-icon" data-move="down">&darr;</button>' +
        "</div>";
      row.querySelector(".reorder-name").textContent = svc.name;
      list.appendChild(row);
    });
    list.addEventListener("click", handleReorderClick);
  }

  function handleReorderClick(e) {
    var btn = e.target.closest("[data-move]");
    if (!btn) return;
    var row = btn.closest(".reorder-row");
    var list = row.parentElement;
    if (btn.dataset.move === "up" && row.previousElementSibling) {
      list.insertBefore(row, row.previousElementSibling);
    } else if (btn.dataset.move === "down" && row.nextElementSibling) {
      list.insertBefore(row.nextElementSibling, row);
    }
  }

  // --- Groups ---
  //
  // A service may belong to at most one group, so checking it in one group
  // clears it from any other.

  function renderGroups(groups) {
    var list = $("#groups-list");
    list.innerHTML = "";
    (groups || []).forEach(function (g) { list.appendChild(groupRow(g)); });
    if (!list.children.length) {
      list.innerHTML = "<p class=\"muted\">No groups yet - every service shows on its own.</p>";
    }
  }

  function groupRow(group) {
    var services = window.__availableServices || [];
    var row = document.createElement("div");
    row.className = "group-row";

    var header = document.createElement("div");
    header.className = "group-header";
    var nameInput = document.createElement("input");
    nameInput.type = "text";
    nameInput.className = "group-name";
    nameInput.placeholder = "Group name (e.g. WEB Experience)";
    nameInput.value = (group && group.name) || "";
    header.appendChild(nameInput);

    var controls = document.createElement("div");
    controls.className = "reorder-controls";
    controls.innerHTML =
      '<button type="button" class="btn btn-ghost btn-icon" data-move="up">&uarr;</button>' +
      '<button type="button" class="btn btn-ghost btn-icon" data-move="down">&darr;</button>' +
      '<button type="button" class="btn btn-danger btn-icon" data-remove="1">&times;</button>';
    header.appendChild(controls);
    row.appendChild(header);

    var members = document.createElement("div");
    members.className = "group-members";
    services.forEach(function (svc) {
      var label = document.createElement("label");
      label.className = "checkbox-row";
      var cb = document.createElement("input");
      cb.type = "checkbox";
      cb.className = "group-member";
      cb.value = svc.id;
      cb.checked = !!(group && (group.services || []).indexOf(svc.id) !== -1);
      var span = document.createElement("span");
      span.textContent = svc.name;
      label.appendChild(cb);
      label.appendChild(span);
      members.appendChild(label);
    });
    row.appendChild(members);
    return row;
  }

  $("#add-group").addEventListener("click", function () {
    var list = $("#groups-list");
    if (!$(".group-row", list)) list.innerHTML = "";
    list.appendChild(groupRow(null));
  });

  $("#groups-list").addEventListener("click", function (e) {
    var row = e.target.closest(".group-row");
    if (!row) return;
    var list = row.parentElement;
    if (e.target.dataset.remove) {
      row.remove();
      if (!$(".group-row", list)) renderGroups([]);
      return;
    }
    if (e.target.dataset.move === "up" && row.previousElementSibling) {
      list.insertBefore(row, row.previousElementSibling);
    } else if (e.target.dataset.move === "down" && row.nextElementSibling) {
      list.insertBefore(row.nextElementSibling, row);
    }
  });

  $("#groups-list").addEventListener("change", function (e) {
    if (!e.target.classList.contains("group-member") || !e.target.checked) return;
    // Clear this service from every other group.
    var owner = e.target.closest(".group-row");
    $all("#groups-list .group-member").forEach(function (cb) {
      if (cb !== e.target && cb.value === e.target.value && cb.closest(".group-row") !== owner) {
        cb.checked = false;
      }
    });
  });

  function collectGroups() {
    return $all("#groups-list .group-row").map(function (row) {
      return {
        name: row.querySelector(".group-name").value.trim(),
        services: $all(".group-member", row)
          .filter(function (cb) { return cb.checked; })
          .map(function (cb) { return cb.value; }),
      };
    }).filter(function (g) { return g.name !== ""; });
  }

  $("#save-services").addEventListener("click", function () {
    var indicator = $("#save-services-indicator");
    var pageId = $("#status-page-select").value;
    var pageName = $("#status-page-select").selectedOptions[0]
      ? $("#status-page-select").selectedOptions[0].textContent
      : "";
    var order = $all("#service-order-list .reorder-row").map(function (r) { return r.dataset.id; });
    var groups = collectGroups();
    var showSub = $("#show-sub-services").checked;
    api("/admin/api/services", {
      method: "POST",
      body: JSON.stringify({
        status_page_id: pageId,
        status_page_name: pageName,
        service_order: order,
        service_groups: groups,
        show_sub_services: showSub,
      }),
    })
      .then(function () {
        window.__savedStatusPageId = pageId;
        window.__savedServiceOrder = order;
        window.__savedGroups = groups;
        flash(indicator, true);
      })
      .catch(function (err) { flash(indicator, false, err.message); });
  });

  // --- Branding ---

  // Mirrors the display page's own fallback: a text colour that can't be
  // read on the chosen background is replaced rather than shown, so the
  // preview tells the truth about what the wall will render.
  var SCHEME_BG = { light: "#f4f5f7", dark: "#101114" };
  var SCHEME_TEXT = { light: "#1a1a1a", dark: "#eceef2" };
  var SCHEME_CARD = { light: "#ffffff", dark: "#191b1f" };

  function luminance(hex) {
    var m = /^#?([0-9a-f]{6})$/i.exec(String(hex).trim());
    if (!m) return null;
    var n = parseInt(m[1], 16);
    var ch = [(n >> 16) & 255, (n >> 8) & 255, n & 255].map(function (c) {
      c = c / 255;
      return c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
    });
    return 0.2126 * ch[0] + 0.7152 * ch[1] + 0.0722 * ch[2];
  }

  function contrast(a, b) {
    var la = luminance(a), lb = luminance(b);
    if (la === null || lb === null) return null;
    return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05);
  }

  function resolveScheme(configured) {
    if (configured === "dark" || configured === "light") return configured;
    return window.matchMedia &&
      window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
  }

  function updatePreview() {
    var primary = $("#primary-color").value;
    var chosenText = $("#text-color").value;
    var isGray = $("#theme-mode").value === "grayscale";
    var scheme = resolveScheme($("#color-scheme").value);

    var ratio = contrast(chosenText, SCHEME_BG[scheme]);
    var readable = ratio !== null && ratio >= 4.5;
    var text = readable ? chosenText : SCHEME_TEXT[scheme];

    var warning = $("#contrast-warning");
    warning.hidden = readable;

    [$("#preview-card"), $("#preview-overall")].forEach(function (node) {
      node.style.setProperty("--primary", primary);
      node.style.setProperty("--pv-text", text);
      node.style.background = SCHEME_CARD[scheme];
      node.style.filter = isGray ? "grayscale(100%)" : "none";
    });

    $("#preview-text").style.color = text;
    $("#preview-overall-title").style.color = text;
    $(".preview-dot", $("#preview-card")).style.background = primary;
    $("#preview-banner").style.filter = isGray ? "grayscale(100%)" : "none";

    var overallPreview = $("#preview-overall");
    overallPreview.dataset.align = $("#overall-align").value;
    overallPreview.dataset.size = $("#overall-size").value;
    overallPreview.dataset.font = $("#font-family").value;
  }

  ["primary-color", "text-color", "theme-mode", "color-scheme", "columns", "banner-fit-mode", "overall-align", "overall-size", "font-family"].forEach(function (id) {
    $("#" + id).addEventListener("input", updatePreview);
  });

  $("#logo-file").addEventListener("change", function () {
    uploadImage(this.files[0], "logo");
  });
  $("#banner-file").addEventListener("change", function () {
    uploadImage(this.files[0], "banner");
  });

  function uploadImage(file, kind) {
    if (!file) return;
    var form = new FormData();
    form.append("file", file);
    fetch("/admin/api/branding/upload?kind=" + kind, { method: "POST", body: form })
      .then(function (res) { return res.json(); })
      .then(function (data) {
        if (data.url) {
          var img = kind === "logo" ? $("#preview-logo-img") : $("#preview-banner-img");
          img.src = data.url + "?t=" + Date.now();
          img.hidden = false;
        }
      });
  }

  $("#save-branding").addEventListener("click", function () {
    var indicator = $("#save-branding-indicator");
    api("/admin/api/branding", {
      method: "POST",
      body: JSON.stringify({
        banner_fit_mode: $("#banner-fit-mode").value,
        theme_mode: $("#theme-mode").value,
        color_scheme: $("#color-scheme").value,
        columns: parseInt($("#columns").value, 10) || 0,
        primary_color: $("#primary-color").value,
        text_color: $("#text-color").value,
        overall_align: $("#overall-align").value,
        overall_size: $("#overall-size").value,
        font_family: $("#font-family").value,
      }),
    })
      .then(function () { flash(indicator, true); })
      .catch(function (err) { flash(indicator, false, err.message); });
  });

  // --- Buttons ---

  function renderButtons(buttons) {
    var list = $("#buttons-list");
    list.innerHTML = "";
    buttons.forEach(function (btn) { list.appendChild(buttonRow(btn)); });
  }

  function buttonRow(btn) {
    btn = btn || {};
    var row = document.createElement("div");
    row.className = "reorder-row button-row";
    row.innerHTML =
      '<input type="text" placeholder="Label" class="btn-label">' +
      '<input type="text" placeholder="https://..." class="btn-url">' +
      '<select class="btn-style" title="Style">' +
      '<option value="">Style: inherit</option>' +
      '<option value="solid">Solid</option>' +
      '<option value="outline">Outline</option>' +
      '<option value="subtle">Subtle</option>' +
      "</select>" +
      '<label class="btn-color-wrap" title="Custom color">' +
      '<input type="color" class="btn-color" value="#e30000">' +
      '<input type="checkbox" class="btn-color-on"><span>Custom</span>' +
      "</label>" +
      '<div class="reorder-controls">' +
      '<button type="button" class="btn btn-ghost btn-icon" data-move="up">&uarr;</button>' +
      '<button type="button" class="btn btn-ghost btn-icon" data-move="down">&darr;</button>' +
      '<button type="button" class="btn btn-danger btn-icon" data-remove="1">&times;</button>' +
      "</div>";
    row.querySelector(".btn-label").value = btn.label || "";
    row.querySelector(".btn-url").value = btn.url || "";
    row.querySelector(".btn-style").value = btn.style || "";
    if (btn.color) {
      row.querySelector(".btn-color").value = btn.color;
      row.querySelector(".btn-color-on").checked = true;
    }
    return row;
  }

  $("#add-button").addEventListener("click", function () {
    $("#buttons-list").appendChild(buttonRow(null));
  });

  $("#buttons-list").addEventListener("click", function (e) {
    var row = e.target.closest(".reorder-row");
    if (!row) return;
    if (e.target.dataset.remove) { row.remove(); return; }
    handleReorderClick(e);
  });

  function updateButtonsPreview() {
    var preview = $("#buttons-preview");
    preview.dataset.align = $("#button-align").value;
    preview.dataset.size = $("#button-size").value;
    preview.style.setProperty("--primary", $("#primary-color").value);
    preview.innerHTML = "";

    var rows = $all("#buttons-list .reorder-row");
    if (!rows.length) {
      preview.innerHTML = "<span class=\"muted\">Add a button to see it here.</span>";
      return;
    }
    var rowStyle = $("#button-style").value;
    rows.forEach(function (row) {
      var label = row.querySelector(".btn-label").value.trim();
      if (!label) return;
      var a = document.createElement("span");
      a.className = "wall-button-preview";
      a.textContent = label;
      a.dataset.style = row.querySelector(".btn-style").value || rowStyle;
      if (row.querySelector(".btn-color-on").checked) {
        a.style.setProperty("--primary", row.querySelector(".btn-color").value);
      }
      preview.appendChild(a);
    });
  }

  ["button-position", "button-align", "button-style", "button-size"].forEach(function (id) {
    $("#" + id).addEventListener("input", updateButtonsPreview);
  });
  $("#buttons-list").addEventListener("input", updateButtonsPreview);
  $("#buttons-list").addEventListener("change", updateButtonsPreview);
  $("#add-button").addEventListener("click", updateButtonsPreview);

  $("#save-buttons").addEventListener("click", function () {
    var indicator = $("#save-buttons-indicator");
    var buttons = $all("#buttons-list .reorder-row").map(function (row) {
      var b = {
        label: row.querySelector(".btn-label").value.trim(),
        url: row.querySelector(".btn-url").value.trim(),
      };
      var style = row.querySelector(".btn-style").value;
      if (style) b.style = style;
      if (row.querySelector(".btn-color-on").checked) {
        b.color = row.querySelector(".btn-color").value;
      }
      return b;
    }).filter(function (b) { return b.label && b.url; });
    api("/admin/api/buttons", {
      method: "POST",
      body: JSON.stringify({
        buttons: buttons,
        button_position: $("#button-position").value,
        button_align: $("#button-align").value,
        button_style: $("#button-style").value,
        button_size: $("#button-size").value,
      }),
    })
      .then(function () { flash(indicator, true); })
      .catch(function (err) { flash(indicator, false, err.message); });
  });

  // --- Security: password ---

  $("#form-password").addEventListener("submit", function (e) {
    e.preventDefault();
    var indicator = $("#save-password");
    api("/admin/api/security/password", {
      method: "POST",
      body: JSON.stringify({
        current_password: $("#current-password").value,
        new_password: $("#new-password").value,
        confirm_password: $("#confirm-password").value,
      }),
    })
      .then(function () {
        flash(indicator, true);
        $("#form-password").reset();
      })
      .catch(function (err) { flash(indicator, false, err.message); });
  });

  // --- Security: network / CIDR allowlist ---

  function renderCidrList(cidrs) {
    var list = $("#cidr-list");
    list.innerHTML = "";
    cidrs.forEach(function (c) { list.appendChild(cidrRow(c)); });
  }

  function cidrRow(value) {
    var row = document.createElement("div");
    row.className = "reorder-row";
    row.innerHTML =
      '<input type="text" placeholder="10.0.0.0/8" class="cidr-value">' +
      '<div class="reorder-controls">' +
      '<button type="button" class="btn btn-danger btn-icon" data-remove="1">&times;</button>' +
      "</div>";
    row.querySelector(".cidr-value").value = value || "";
    return row;
  }

  $("#add-cidr").addEventListener("click", function () {
    $("#cidr-list").appendChild(cidrRow(""));
  });

  $("#cidr-list").addEventListener("click", function (e) {
    if (e.target.dataset.remove) { e.target.closest(".reorder-row").remove(); }
  });

  $("#save-network").addEventListener("click", function () {
    var indicator = $("#save-network-indicator");
    var cidrs = $all("#cidr-list .cidr-value").map(function (i) { return i.value.trim(); }).filter(Boolean);
    api("/admin/api/security/network", { method: "POST", body: JSON.stringify({ cidr_allowlist: cidrs }) })
      .then(function () { flash(indicator, true); })
      .catch(function (err) { flash(indicator, false, err.message); });
  });

  boot();
})();

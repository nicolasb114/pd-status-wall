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
    $("#primary-color").value = cfg.primary_color || "#e30000";
    $("#text-color").value = cfg.text_color || "#1a1a1a";
    $("#overall-align").value = cfg.overall_align || "left";
    $("#overall-size").value = cfg.overall_size || "medium";
    $("#font-family").value = cfg.font_family || "system";
    updatePreview();

    renderButtons(cfg.buttons || []);
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

  function updatePreview() {
    var primary = $("#primary-color").value;
    var text = $("#text-color").value;
    var isGray = $("#theme-mode").value === "grayscale";
    var preview = $("#preview-card");
    preview.style.setProperty("--primary", primary);
    preview.style.setProperty("--pv-text", text);
    $("#preview-text").style.color = text;
    $(".preview-dot", preview).style.background = primary;
    $("#preview-banner").style.filter = isGray ? "grayscale(100%)" : "none";
    preview.style.filter = isGray ? "grayscale(100%)" : "none";

    var overallPreview = $("#preview-overall");
    overallPreview.style.setProperty("--primary", primary);
    overallPreview.style.setProperty("--pv-text", text);
    overallPreview.dataset.align = $("#overall-align").value;
    overallPreview.dataset.size = $("#overall-size").value;
    overallPreview.dataset.font = $("#font-family").value;
    overallPreview.style.filter = isGray ? "grayscale(100%)" : "none";
  }

  ["primary-color", "text-color", "theme-mode", "banner-fit-mode", "overall-align", "overall-size", "font-family"].forEach(function (id) {
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
    buttons.forEach(function (btn) { list.appendChild(buttonRow(btn.label, btn.url)); });
  }

  function buttonRow(label, url) {
    var row = document.createElement("div");
    row.className = "reorder-row";
    row.innerHTML =
      '<input type="text" placeholder="Label" class="btn-label" style="max-width:180px">' +
      '<input type="text" placeholder="https://..." class="btn-url">' +
      '<div class="reorder-controls">' +
      '<button type="button" class="btn btn-ghost btn-icon" data-move="up">&uarr;</button>' +
      '<button type="button" class="btn btn-ghost btn-icon" data-move="down">&darr;</button>' +
      '<button type="button" class="btn btn-danger btn-icon" data-remove="1">&times;</button>' +
      "</div>";
    row.querySelector(".btn-label").value = label || "";
    row.querySelector(".btn-url").value = url || "";
    return row;
  }

  $("#add-button").addEventListener("click", function () {
    $("#buttons-list").appendChild(buttonRow("", ""));
  });

  $("#buttons-list").addEventListener("click", function (e) {
    var row = e.target.closest(".reorder-row");
    if (!row) return;
    if (e.target.dataset.remove) { row.remove(); return; }
    handleReorderClick(e);
  });

  $("#save-buttons").addEventListener("click", function () {
    var indicator = $("#save-buttons-indicator");
    var buttons = $all("#buttons-list .reorder-row").map(function (row) {
      return { label: row.querySelector(".btn-label").value, url: row.querySelector(".btn-url").value };
    }).filter(function (b) { return b.label && b.url; });
    api("/admin/api/buttons", { method: "POST", body: JSON.stringify({ buttons: buttons }) })
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

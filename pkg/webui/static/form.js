// form.js — Client-side logic for schema-driven forms.
// Handles: JSON serialization, conditional visibility, array/map fields, errors.

(function () {
  "use strict";

  // --- Conditional Visibility ---
  function initConditionalFields() {
    document.querySelectorAll("[data-depends-on]").forEach(function (el) {
      var depName = el.dataset.dependsOn;
      var showWhen = (el.dataset.showWhen || "").split(",");
      var dep = document.getElementsByName(depName)[0];
      if (!dep) return;

      function update() {
        el.style.display = showWhen.indexOf(dep.value) !== -1 ? "" : "none";
        // Disable hidden required fields so they don't block submit
        el.querySelectorAll("[required]").forEach(function (input) {
          input.disabled = showWhen.indexOf(dep.value) === -1;
        });
      }

      dep.addEventListener("change", update);
      update();
    });
  }

  // --- Array Fields ---
  function initArrayFields() {
    document.querySelectorAll("[data-array-add]").forEach(function (btn) {
      btn.addEventListener("click", function () {
        var container = btn.closest("[data-array-field]");
        var template = container.querySelector("[data-array-template]");
        var entries = container.querySelector("[data-array-entries]");
        if (!template || !entries) return;

        var clone = template.content.cloneNode(true);
        var idx = entries.children.length;

        // Update placeholder names: replace * with index
        clone.querySelectorAll("[name]").forEach(function (input) {
          input.name = input.name.replace("*", idx.toString());
        });

        // Wire remove button
        var removeBtn = clone.querySelector("[data-array-remove]");
        if (removeBtn) {
          removeBtn.addEventListener("click", function () {
            var item = this.closest("[data-array-item]");
            var parent = item.parentNode;
            item.remove();
            reindexArrayItems(parent);
          });
        }

        entries.appendChild(clone);
      });
    });
  }

  // Re-index array items after removal to avoid sparse arrays.
  function reindexArrayItems(entries) {
    var items = entries.querySelectorAll("[data-array-item]");
    items.forEach(function (item, idx) {
      item.querySelectorAll("[name]").forEach(function (input) {
        // Replace the numeric index in dot-notation names (e.g., endpoints.2.name -> endpoints.0.name)
        input.name = input.name.replace(/\.\d+\./, "." + idx + ".");
      });
    });
  }

  // --- Map Fields (key-value pairs) ---
  function initMapFields() {
    document.querySelectorAll("[data-map-add]").forEach(function (btn) {
      var fieldName = btn.dataset.mapAdd;
      btn.addEventListener("click", function () {
        var entries = btn
          .closest("[data-map-field]")
          .querySelector("[data-map-entries]");
        if (!entries) return;

        var row = document.createElement("div");
        row.style.cssText =
          "display:flex;gap:8px;margin-bottom:6px;align-items:center;";
        row.innerHTML =
          '<input type="text" data-map-key placeholder="Key" ' +
          'style="flex:1;padding:6px 8px;background:var(--bg-tertiary);color:var(--text-primary);' +
          'border:1px solid var(--border);border-radius:4px;font-size:13px;"/>' +
          '<input type="text" data-map-value data-map-name="' +
          fieldName +
          '" placeholder="Value" ' +
          'style="flex:1;padding:6px 8px;background:var(--bg-tertiary);color:var(--text-primary);' +
          'border:1px solid var(--border);border-radius:4px;font-size:13px;"/>' +
          '<button type="button" data-map-remove ' +
          'style="font-size:11px;color:var(--error);background:none;border:none;cursor:pointer;">' +
          "Remove</button>";

        row.querySelector("[data-map-remove]").addEventListener(
          "click",
          function () {
            row.remove();
          }
        );

        entries.appendChild(row);
      });
    });
  }

  // --- JSON Serialization ---
  // Collects form data into a nested JSON object using dot-notation field names.
  function serializeForm(form) {
    var data = {};
    var inputs = form.querySelectorAll("input, select, textarea");

    inputs.forEach(function (input) {
      if (input.disabled || !input.name || input.closest("template")) return;

      var value;
      if (input.type === "checkbox") {
        value = input.checked;
      } else if (input.type === "number") {
        value = input.value === "" ? undefined : Number(input.value);
      } else {
        value = input.value;
      }

      // In edit mode (PUT), include empty strings to allow clearing optional fields.
      // In create mode (POST), skip empty values.
      var isEdit = form.hasAttribute("hx-put");
      if (value === undefined) return;
      if (value === "" && !isEdit) return;

      setNestedValue(data, input.name, value);
    });

    // Collect map fields (scoped to the form)
    form
      .querySelectorAll("[data-map-field]")
      .forEach(function (container) {
        var mapName = container.dataset.mapField;
        var map = {};
        container
          .querySelectorAll("[data-map-entries] > div")
          .forEach(function (row) {
            var key = row.querySelector("[data-map-key]");
            var val = row.querySelector("[data-map-value]");
            if (key && val && key.value && val.value) {
              map[key.value] = val.value;
            }
          });
        if (Object.keys(map).length > 0) {
          setNestedValue(data, mapName, map);
        }
      });

    return data;
  }

  function setNestedValue(obj, path, value) {
    var parts = path.split(".");
    var current = obj;
    for (var i = 0; i < parts.length - 1; i++) {
      var key = parts[i];
      if (!(key in current) || typeof current[key] !== "object") {
        // Check if next key is numeric (array index)
        current[key] = /^\d+$/.test(parts[i + 1]) ? [] : {};
      }
      current = current[key];
    }
    current[parts[parts.length - 1]] = value;
  }

  // --- HTMX Integration ---
  // Custom htmx extension that serializes forms as nested JSON.
  // Replaces htmx-ext-json-enc for forms with dot-notation field names.
  if (typeof htmx !== "undefined") {
    htmx.defineExtension("json-enc", {
      onEvent: function (name, evt) {
        if (name === "htmx:configRequest") {
          evt.detail.headers["Content-Type"] = "application/json";
        }
      },
      encodeParameters: function (_xhr, parameters, elt) {
        var form = elt.closest("#resource-form");
        if (form) {
          return JSON.stringify(serializeForm(form));
        }
        return JSON.stringify(parameters);
      },
    });
  }

  // Handle errors from API
  document.addEventListener("htmx:responseError", function (evt) {
    var target = document.getElementById("form-result");
    if (!target) return;

    var xhr = evt.detail.xhr;
    var message = "Request failed";

    try {
      var body = JSON.parse(xhr.responseText);
      if (body.error) message = body.error;
    } catch (_e) {
      message = xhr.statusText || message;
    }

    target.innerHTML =
      '<div style="padding:12px;background:rgba(244,67,54,0.1);' +
      "border:1px solid var(--error);border-radius:6px;color:var(--error);" +
      'font-size:13px;margin-bottom:16px;">' +
      escapeHtml(message) +
      "</div>";
  });

  // Handle success — redirect
  document.addEventListener("htmx:afterRequest", function (evt) {
    if (!evt.detail.successful) return;

    var form = evt.detail.elt;
    if (!form || !form.matches || !form.matches("#resource-form")) return;

    var redirect = form.dataset.successRedirect;
    if (redirect) {
      window.location.href = redirect;
    }
  });

  function escapeHtml(text) {
    var div = document.createElement("div");
    div.appendChild(document.createTextNode(text));
    return div.innerHTML;
  }

  // --- ConfigMap Picker ---
  // Populates configMapRef select dropdowns from /ui/api/configmaps
  function initConfigMapPickers() {
    document.querySelectorAll("[data-configmap-ref]").forEach(function (container) {
      var nameSelect = container.querySelector("[data-configmap-name]");
      var keySelect = container.querySelector("[data-configmap-key]");
      var createBtn = container.querySelector("[data-configmap-create]");
      if (!nameSelect) return;

      // Detect namespace from the form's namespace field
      var form = container.closest("form");
      var nsInput = form ? form.querySelector('[name="namespace"]') : null;

      function loadConfigMaps() {
        var ns = nsInput ? nsInput.value : "";
        if (!ns) return;

        fetch("/ui/api/configmaps?namespace=" + encodeURIComponent(ns))
          .then(function (r) { return r.json(); })
          .then(function (cms) {
            var current = nameSelect.value;
            nameSelect.innerHTML = '<option value="">— Select ConfigMap —</option>';
            (cms || []).forEach(function (cm) {
              var opt = document.createElement("option");
              opt.value = cm.Name;
              opt.textContent = cm.Name;
              opt.dataset.keys = JSON.stringify(cm.Keys || []);
              if (cm.Name === current) opt.selected = true;
              nameSelect.appendChild(opt);
            });
            updateKeys();
          })
          .catch(function () { /* silent — API may not be available */ });
      }

      function updateKeys() {
        if (!keySelect) return;
        var selected = nameSelect.options[nameSelect.selectedIndex];
        var keys = [];
        try { keys = JSON.parse(selected ? selected.dataset.keys || "[]" : "[]"); } catch (_e) { /* noop */ }

        var currentKey = keySelect.value;
        keySelect.innerHTML = '<option value="">— Select Key —</option>';
        keys.forEach(function (k) {
          var opt = document.createElement("option");
          opt.value = k;
          opt.textContent = k;
          if (k === currentKey) opt.selected = true;
          keySelect.appendChild(opt);
        });
      }

      nameSelect.addEventListener("change", updateKeys);
      if (nsInput) nsInput.addEventListener("change", loadConfigMaps);

      // Initial load
      loadConfigMaps();

      // Create New ConfigMap inline
      if (createBtn) {
        createBtn.addEventListener("click", function () {
          var ns = nsInput ? nsInput.value : "";
          if (!ns) { alert("Select a namespace first"); return; }

          var cmName = prompt("ConfigMap name:");
          if (!cmName) return;
          var key = prompt("Key (filename):");
          if (!key) return;
          var content = prompt("File content:");
          if (content === null) return;

          var body = { name: cmName, namespace: ns, data: {} };
          body.data[key] = content;

          fetch("/ui/api/configmaps/create", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(body)
          })
            .then(function (r) {
              if (!r.ok) throw new Error("Failed to create ConfigMap");
              return r.json();
            })
            .then(function () {
              loadConfigMaps();
              // Auto-select the new ConfigMap after reload
              setTimeout(function () {
                nameSelect.value = cmName;
                updateKeys();
                if (keySelect) keySelect.value = key;
              }, 500);
            })
            .catch(function (err) { alert(err.message); });
        });
      }
    });
  }

  // --- Init ---
  function init() {
    initConditionalFields();
    initArrayFields();
    initMapFields();
    initConfigMapPickers();
  }

  // Run on DOM ready and after HTMX swaps
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
  document.addEventListener("htmx:afterSettle", init);
})();

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
            this.closest("[data-array-item]").remove();
          });
        }

        entries.appendChild(clone);
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

      if (value === undefined || value === "") return;

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

  // --- Init ---
  function init() {
    initConditionalFields();
    initArrayFields();
    initMapFields();
  }

  // Run on DOM ready and after HTMX swaps
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
  document.addEventListener("htmx:afterSettle", init);
})();

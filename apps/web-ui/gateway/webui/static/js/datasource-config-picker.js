// Suggestion chips for a saved data source's config fields.
//
// The picker's listing comes from the gateway (/data-sources/:id/field-options),
// which lists through the source's STORED credentials — the edit form never
// carries credential inputs, so the browser cannot re-submit them. The HTML
// fragment returns the provider's values as buttons tagged data-ds-suggest;
// this script writes a clicked value into the field it belongs to:
//
//   * an array field (textarea) appends, so a multi-value scope such as Trale
//     tags keeps the values already chosen;
//   * a single-value field (input) is replaced.
//
// Progressive enhancement only: without this script the field stays a plain
// input/textarea and every value can still be typed by hand.
(function () {
  "use strict";

  var SEPARATOR = /\s*[,;\n]\s*/;

  function currentValues(textarea) {
    return textarea.value
      .split(SEPARATOR)
      .map(function (value) {
        return value.trim();
      })
      .filter(function (value) {
        return value.length > 0;
      });
  }

  function appendValue(textarea, value) {
    var values = currentValues(textarea);
    if (values.indexOf(value) !== -1) return;
    values.push(value);
    textarea.value = values.join("\n");
    textarea.dispatchEvent(new Event("input", { bubbles: true }));
  }

  document.addEventListener("click", function (event) {
    var chip = event.target.closest("[data-ds-suggest]");
    if (!chip) return;

    var scope = chip.closest("[data-ds-picker-scope]");
    if (!scope) return;

    var field = scope.querySelector("[data-ds-picker-input]");
    if (!field) return;

    var value = chip.getAttribute("data-ds-suggest");
    if (!value) return;

    event.preventDefault();
    if (field.tagName === "TEXTAREA") {
      appendValue(field, value);
    } else {
      field.value = value;
      field.dispatchEvent(new Event("input", { bubbles: true }));
    }
    // Keep the caret in the field so the user can keep typing suggestions.
    field.focus();
  });
})();

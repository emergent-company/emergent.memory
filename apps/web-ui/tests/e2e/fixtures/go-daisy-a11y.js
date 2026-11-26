/* Synced verbatim from go-daisy staticfs/static/js/go-daisy-a11y.js (go.mod: v0.20.0). Re-copy on a go-daisy upgrade; see dropdown-runtime.spec.ts. */
/*
 * go-daisy a11y primitives.
 *
 * One small, dependency-free layer for the keyboard and focus behaviour that
 * every menu-like widget needs. Components opt in with data attributes and a
 * tiny inline script that calls gdMenu / gdFocus, so the semantics stay in one
 * place instead of being re-implemented per component.
 *
 * Public surface:
 *   gdFocus.trap(container, { onEscape, initial })  -> release()
 *   gdFocus.roving(container, itemSelector)         -> { first, last, next, prev, at, index }
 *   gdFocus.typeahead()                             -> match(items, labelOf, ch)
 *   gdMenu.init(root)                               -> wires one menu root
 *   gdMenu.rescan()                                 -> wires every unwired [data-gd-menu-root]
 *   gdMenu.open(root, toFirst)                      -> opens (focus trigger, optionally first item)
 *   gdMenu.close(root, restoreFocus)                -> closes (optionally restore focus to trigger)
 *   gdMenu.isOpen(root)                             -> true when open (:focus-within or marker)
 *
 * Menu contract (attribute driven):
 *   [data-gd-menu-root]                 container; may be any element
 *   [data-gd-menu-trigger]              button/role=button that toggles
 *   [data-gd-menu]                      the menu surface (role=menu)
 *   [data-gd-menu-item] or role=menuitem
 *   [data-gd-menu-keep-closed]          optional: never clear the closed marker
 *                                       on focus-leave (composed owners such as
 *                                       the menubar own when a menu may open)
 *
 * A root without [data-gd-menu-trigger] is not wired, and the runtime leaves it
 * to daisyUI's CSS-only (:focus-within) behaviour: it never closes it and never
 * stamps the closed marker on it. Use [data-gd-menu-trigger] to opt into the
 * keyboard/focus upgrade.
 *
 * Behaviour: ArrowDown/Up/Home/End roving focus, Enter/Space activation,
 * Escape closes and restores focus, Tab closes and leaves, printable-key
 * typeahead, click-outside closes, aria-expanded kept in sync.
 */
(function () {
  'use strict';

  if (window.gdFocus && window.gdMenu) return;

  var SEARCH_RESET_MS = 600;

  function isVisible(el) {
    if (!el || el.nodeType !== 1) return false;
    if (el.hasAttribute('disabled') || el.getAttribute('aria-disabled') === 'true') return false;
    if (el.hidden) return false;
    var style = window.getComputedStyle(el);
    return style.display !== 'none' && style.visibility !== 'hidden';
  }

  function focusableIn(container) {
    var sel = 'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]),' +
      ' textarea:not([disabled]), [tabindex]:not([tabindex="-1"]), [contenteditable="true"]';
    return Array.prototype.filter.call(container.querySelectorAll(sel), isVisible);
  }

  /* ---------------------------------------------------------------- gdFocus */

  var gdFocus = window.gdFocus || {};

  /**
   * trap(container, { onEscape, initial }) keeps Tab focus inside container,
   * focuses `initial` (or the first focusable child), and returns release()
   * which restores the previously focused element.
   */
  gdFocus.trap = function (container, opts) {
    if (!container) return function () {};
    opts = opts || {};
    var previous = document.activeElement;
    var target = opts.initial || focusableIn(container)[0] || container;

    if (!container.hasAttribute('tabindex') && target === container) {
      container.setAttribute('tabindex', '-1');
    }
    try { target.focus(); } catch (e) {}

    function onKeydown(e) {
      if (e.key === 'Escape' && opts.onEscape) {
        e.stopPropagation();
        opts.onEscape(e);
        return;
      }
      if (e.key !== 'Tab') return;
      var items = focusableIn(container);
      if (!items.length) { e.preventDefault(); return; }
      var first = items[0];
      var last = items[items.length - 1];
      if (e.shiftKey && (document.activeElement === first || document.activeElement === container)) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    }

    container.addEventListener('keydown', onKeydown);

    function release() {
      container.removeEventListener('keydown', onKeydown);
      if (opts.restore !== false && previous && previous.focus) {
        try { previous.focus(); } catch (e) {}
      }
    }
    return release;
  };

  /**
   * roving(container, itemSelector) implements the WAI-ARIA roving tabindex
   * pattern: exactly one item is tabbable, arrows move focus with wraparound.
   */
  gdFocus.roving = function (container, itemSelector) {
    var selector = itemSelector || '[data-gd-menu-item], [role="menuitem"], [role="option"]';

    function items() {
      return Array.prototype.filter.call(container.querySelectorAll(selector), isVisible);
    }

    function at(index) {
      var list = items();
      if (!list.length) return null;
      var i = ((index % list.length) + list.length) % list.length;
      for (var n = 0; n < list.length; n++) {
        list[n].setAttribute('tabindex', n === i ? '0' : '-1');
      }
      try { list[i].focus(); } catch (e) {}
      return list[i];
    }

    function current() {
      var list = items();
      var index = list.indexOf(document.activeElement);
      return index;
    }

    function prime() {
      var list = items();
      list.forEach(function (el, i) {
        if (!el.hasAttribute('tabindex')) el.setAttribute('tabindex', i === 0 ? '0' : '-1');
      });
      return list;
    }

    return {
      items: items,
      prime: prime,
      index: current,
      at: at,
      first: function () { return at(0); },
      last: function () { return at(-1); },
      next: function () { return at(current() + 1); },
      prev: function () { return at(current() - 1); },
    };
  };

  /**
   * typeahead() returns a stateful matcher: feed it each printable character
   * and it resolves the next item whose text starts with the accumulated
   * buffer. Resets after SEARCH_RESET_MS of inactivity.
   */
  gdFocus.typeahead = function () {
    var buffer = '';
    var last = 0;
    return function (items, labelOf, ch) {
      var now = Date.now();
      if (now - last > SEARCH_RESET_MS) buffer = '';
      last = now;
      buffer += ch.toLowerCase();
      var needle = buffer;
      // Repeated identical characters cycle: "ddd" moves to the third d-item.
      var repeated = buffer.split('').every(function (c) { return c === buffer[0]; });
      var start = 0;
      if (repeated && buffer.length > 1) {
        needle = buffer[0];
        var active = items.indexOf(document.activeElement);
        start = active >= 0 ? active + 1 : 0;
      }
      for (var n = 0; n < items.length; n++) {
        var item = items[(start + n) % items.length];
        var label = (labelOf ? labelOf(item) : item.textContent) || '';
        if (label.trim().toLowerCase().indexOf(needle) === 0) return item;
      }
      return null;
    };
  };

  window.gdFocus = gdFocus;

  /* ----------------------------------------------------------------- gdMenu */

  function triggerOf(root) { return root.querySelector('[data-gd-menu-trigger]'); }
  function menuOf(root) { return root.querySelector('[data-gd-menu]'); }

  function setExpanded(root, open) {
    var trigger = triggerOf(root);
    if (trigger) trigger.setAttribute('aria-expanded', open ? 'true' : 'false');
    if (open) root.setAttribute('data-gd-menu-open', ''); else root.removeAttribute('data-gd-menu-open');
  }

  function close(root, restoreFocus) {
    if (!root) return;
    var trigger = triggerOf(root);
    // daisyUI opens on :focus-within, so restoring focus to the trigger would
    // immediately re-open the menu. The attribute suppresses that one case and
    // is cleared as soon as the user interacts again.
    root.setAttribute('data-gd-menu-closed', '');
    setExpanded(root, false);
    var active = document.activeElement;
    if (active && root.contains(active)) active.blur();
    if (restoreFocus && trigger && trigger.focus) {
      try { trigger.focus(); } catch (e) {}
    }
  }

  function open(root, toFirst) {
    if (!root) return;
    var trigger = triggerOf(root);
    root.removeAttribute('data-gd-menu-closed');
    if (trigger) {
      try { trigger.focus(); } catch (e) {}
    }
    setExpanded(root, true);
    if (toFirst) {
      var menu = menuOf(root);
      if (menu) gdFocus.roving(menu).first();
    }
  }

  // A menu counts as open when its explicit open marker is set, when focus is
  // anywhere inside it, or when it matches :focus-within (the CSS-driven state
  // daisyUI uses). The explicit closed marker wins over all of them: close()
  // stamps it before restoring focus to the trigger, and without this check the
  // focus-based clauses would immediately report the just-closed menu as open
  // again (that is what re-opened a menubar menu on a second trigger click).
  // The menubar runtime deliberately does NOT use this predicate for "is any
  // menu open": it reads data-gd-menu-open directly, because a closed menu may
  // legitimately hold the trigger's focus.
  function isOpen(root) {
    if (!root || root.hasAttribute('data-gd-menu-closed')) return false;
    return root.hasAttribute('data-gd-menu-open') ||
      root.matches(':focus-within') ||
      root.contains(document.activeElement);
  }

  function wire(root) {
    if (root.getAttribute('data-gd-menu-wired') === 'true') return;
    var trigger = triggerOf(root);
    var menu = menuOf(root);
    if (!trigger || !menu) return;
    root.setAttribute('data-gd-menu-wired', 'true');

    var roving = gdFocus.roving(menu);
    roving.prime();
    var typeahead = gdFocus.typeahead();
    trigger.setAttribute('aria-expanded', 'false');
    if (!trigger.hasAttribute('aria-haspopup')) trigger.setAttribute('aria-haspopup', 'menu');

    function items() { return roving.items(); }
    function labelOf(el) { return el.getAttribute('data-gd-menu-label') || el.textContent; }

    trigger.addEventListener('keydown', function (e) {
      if (e.key === 'ArrowDown' || e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        open(root, true);
      } else if (e.key === 'ArrowUp') {
        e.preventDefault();
        open(root, false);
        roving.last();
      } else if (e.key === 'Escape' && isOpen(root)) {
        e.preventDefault();
        close(root, true);
      }
    });

    trigger.addEventListener('pointerdown', function () {
      root.removeAttribute('data-gd-menu-closed');
    });

    trigger.addEventListener('click', function () {
      // The trigger owns focus on click; reflect it in aria-expanded.
      window.setTimeout(function () { setExpanded(root, isOpen(root)); }, 0);
    });

    menu.addEventListener('keydown', function (e) {
      var list = items();
      if (!list.length) return;
      var handled = true;
      switch (e.key) {
        case 'ArrowDown': roving.next(); break;
        case 'ArrowUp': roving.prev(); break;
        case 'Home': roving.first(); break;
        case 'End': roving.last(); break;
        case 'Escape': close(root, true); break;
        case 'Tab': close(root, false); handled = false; break;
        default:
          if (e.key.length === 1 && !e.ctrlKey && !e.metaKey && !e.altKey) {
            var hit = typeahead(list, labelOf, e.key);
            if (hit) { hit.focus(); } else { handled = false; }
          } else {
            handled = false;
          }
      }
      if (handled) e.preventDefault();
    });

    menu.addEventListener('focusout', function (e) {
      var next = e.relatedTarget;
      if (!next || !root.contains(next)) {
        window.setTimeout(function () {
          if (!root.contains(document.activeElement)) setExpanded(root, false);
        }, 0);
      }
    });

    // Reflect the CSS-driven open state (DaisyUI opens on :focus-within), but
    // never while the explicit closed marker is set — that is what makes
    // Escape-then-restore-focus actually stay closed.
    root.addEventListener('focusin', function () {
      if (!root.hasAttribute('data-gd-menu-closed')) setExpanded(root, true);
    });

    root.addEventListener('focusout', function (event) {
      var next = event.relatedTarget;
      if (!next || !root.contains(next)) {
        window.setTimeout(function () {
          if (!root.contains(document.activeElement)) {
            // A composed owner (menubar) that must stay closed until asked can
            // opt out of clearing the marker on focus-leave; otherwise the next
            // focus would auto-open the menu again.
            if (!root.hasAttribute('data-gd-menu-keep-closed')) {
              root.removeAttribute('data-gd-menu-closed');
            }
            setExpanded(root, false);
          }
        }, 0);
      }
    });
  }

  var gdMenu = window.gdMenu || {
    init: wire,
    rescan: function () {
      Array.prototype.forEach.call(
        document.querySelectorAll('[data-gd-menu-root]'),
        wire
      );
    },
    open: function (root, toFirst) { open(root, toFirst); },
    close: function (root, restoreFocus) { close(root, restoreFocus === true); },
    isOpen: function (root) { return isOpen(root); },
  };

  window.gdMenu = gdMenu;

  if (!window.__gdMenuBound) {
    window.__gdMenuBound = true;
    document.addEventListener('htmx:after:settle', gdMenu.rescan);
    document.addEventListener('htmx:afterSettle', gdMenu.rescan);

    // One delegated click listener for every menu root. Registering this per
    // root would leak a listener (and a detached DOM reference) on every htmx
    // swap that re-renders a dropdown.
    //
    // Only roots this runtime actually wired are closed here. An unwired root
    // (no [data-gd-menu-trigger], so wire() bailed) is a plain CSS-only daisyUI
    // dropdown: it already closes when focus leaves it, and stamping
    // data-gd-menu-closed on it would be permanent, because every handler that
    // clears that marker is installed by wire(). That made such a dropdown open
    // exactly once per page load: the first outside click hid the panel for good.
    document.addEventListener('click', function (event) {
      var inside = event.target.closest && event.target.closest('[data-gd-menu-root]');
      Array.prototype.forEach.call(document.querySelectorAll('[data-gd-menu-root]'), function (root) {
        if (root === inside) return;
        if (root.getAttribute('data-gd-menu-wired') !== 'true') return;
        close(root, false);
      });
    });

    // Recovery net for an unwired root that acquired the marker anyway (a page
    // calling gdMenu.close(root) on it, or markup that lost its trigger between
    // swaps). The marker means "the user just closed this"; the next interaction
    // inside the root has to clear it or the dropdown never opens again. Wired
    // roots keep owning their own marker (the menubar deliberately re-stamps it).
    document.addEventListener('pointerdown', function (event) {
      var root = event.target.closest && event.target.closest('[data-gd-menu-root]');
      if (!root || root.getAttribute('data-gd-menu-wired') === 'true') return;
      root.removeAttribute('data-gd-menu-closed');
    });
    document.addEventListener('DOMContentLoaded', gdMenu.rescan);
    // Also wire immediately: a component script injected after DOMContentLoaded
    // (partial swap, lazy chunk) would otherwise never see its rescan run.
    gdMenu.rescan();
  }
})();

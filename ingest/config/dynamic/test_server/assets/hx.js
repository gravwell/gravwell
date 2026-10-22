// A deliberately small stand in for htmx, so that this page needs nothing from a CDN and
// works on an air gapped box.  It implements only the attributes this UI uses:
//
//   hx-get / hx-post   the request to make
//   hx-target          CSS selector for where the response HTML goes, defaults to self
//   hx-swap            innerHTML (default), outerHTML or beforeend
//   hx-trigger         click (default), submit, load, or "every <n>s"
//   hx-confirm         confirm() text before firing
//
// Plus two attributes of its own: data-toggle="<selector>", which shows and hides an
// element on click, and data-remove="<selector>", which removes the nearest matching
// ancestor, used by the minus button on a repeating input.  Real htmx would do this with hx-on or a class swap, it is here
// because a dropdown needs somewhere to keep its open state and the server does not have
// one.
//
// A response may carry X-Refresh: <selector>,<selector> to have those elements re-run
// their hx-get, which is how a save refreshes the lists it changed.
//
// The markup is ordinary htmx, so dropping in the real library and deleting this file
// should be a no-op.
(function () {
	function sel(el, attr) { return el.getAttribute(attr); }

	function targetOf(el) {
		var t = sel(el, 'hx-target');
		return t ? document.querySelector(t) : el;
	}

	function swap(el, html) {
		var target = targetOf(el);
		if (!target) return;
		var how = sel(el, 'hx-swap');
		if (how === 'outerHTML') {
			target.outerHTML = html;
		} else if (how === 'beforeend') {
			target.insertAdjacentHTML('beforeend', html);
		} else {
			target.innerHTML = html;
		}
		bind(document);
	}

	function body(el) {
		// a form sends its own fields, anything else sends nothing
		var form = el.tagName === 'FORM' ? el : el.closest('form');
		if (!form) return null;
		return new URLSearchParams(new FormData(form));
	}

	function fire(el, method, url) {
		var confirmText = sel(el, 'hx-confirm');
		if (confirmText && !window.confirm(confirmText)) return;

		var opts = { method: method, headers: {} };
		if (method === 'POST') {
			var b = body(el);
			opts.body = b ? b.toString() : '';
			opts.headers['Content-Type'] = 'application/x-www-form-urlencoded';
		}
		fetch(url, opts).then(function (resp) {
			var refresh = resp.headers.get('X-Refresh');
			return resp.text().then(function (text) {
				swap(el, text);
				if (refresh) {
					refresh.split(',').forEach(function (s) {
						var node = document.querySelector(s.trim());
						if (node && sel(node, 'hx-get')) fire(node, 'GET', sel(node, 'hx-get'));
					});
				}
			});
		}).catch(function (err) {
			swap(el, '<div class="note bad">request failed: ' + String(err) + '</div>');
		});
	}

	function bindRemovers(root) {
		root.querySelectorAll('[data-remove]').forEach(function (el) {
			if (el.__removeBound) return;
			el.__removeBound = true;
			el.addEventListener('click', function (ev) {
				ev.preventDefault();
				var row = el.closest(el.getAttribute('data-remove'));
				if (row) row.remove();
			});
		});
	}

	function bindToggles(root) {
		root.querySelectorAll('[data-toggle]').forEach(function (el) {
			if (el.__toggleBound) return;
			el.__toggleBound = true;
			el.addEventListener('click', function (ev) {
				ev.preventDefault();
				ev.stopPropagation(); // so the close-on-outside-click below does not undo it
				var target = document.querySelector(el.getAttribute('data-toggle'));
				if (target) target.hidden = !target.hidden;
			});
		});
	}

	// a click anywhere else closes any open dropdown, which is what people expect and
	// what keeps one from sitting over the page forever
	document.addEventListener('click', function (ev) {
		document.querySelectorAll('.dropdown:not([hidden])').forEach(function (d) {
			if (!d.contains(ev.target)) d.hidden = true;
		});
	});
	document.addEventListener('keydown', function (ev) {
		if (ev.key !== 'Escape') return;
		document.querySelectorAll('.dropdown:not([hidden])').forEach(function (d) {
			d.hidden = true;
		});
	});

	function bind(root) {
		bindToggles(root);
		bindRemovers(root);
		root.querySelectorAll('[hx-get],[hx-post]').forEach(function (el) {
			if (el.__hxBound) return;
			el.__hxBound = true;

			var method = sel(el, 'hx-post') ? 'POST' : 'GET';
			var url = sel(el, 'hx-post') || sel(el, 'hx-get');
			var trigger = sel(el, 'hx-trigger') || (el.tagName === 'FORM' ? 'submit' : 'click');

			trigger.split(',').forEach(function (t) {
				t = t.trim();
				var every = t.match(/^every\s+([0-9.]+)s$/);
				if (every) {
					// a fragment that polls can itself be swapped away, and its timer
					// would otherwise keep firing at a detached node forever, once per
					// time the fragment was ever opened
					var iv = setInterval(function () {
						if (!el.isConnected) { clearInterval(iv); return; }
						fire(el, method, url);
					}, parseFloat(every[1]) * 1000);
				} else if (t === 'load') {
					fire(el, method, url);
				} else if (t === 'submit') {
					el.addEventListener('submit', function (ev) { ev.preventDefault(); fire(el, method, url); });
				} else {
					el.addEventListener(t, function (ev) { ev.preventDefault(); fire(el, method, url); });
				}
			});
		});
	}

	document.addEventListener('DOMContentLoaded', function () { bind(document); });
})();

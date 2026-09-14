package main

// toastQueueState returns the Alpine x-data for the toast queue: an empty
// toasts array. go-daisy's alpine.ToastQueueState marshals a nil slice as
// JSON null, which breaks the queue's push — so we seed [] explicitly.
func toastQueueState() map[string]any {
	return map[string]any{"toasts": []any{}}
}

// toastQueueInit returns the Alpine x-init expression for the toast queue.
// Extends go-daisy's ToastQueueInit with hover-to-pause: each toast tracks its
// remaining lifetime, so pausing clears the timer and resuming re-arms it with
// the leftover time (and the progress bar's animation-play-state toggles).
func toastQueueInit() string {
	return `let queue = $data;
if (!queue.toasts) queue.toasts = [];
queue.add = function (t) {
  var item = Object.assign({ id: 't' + Date.now() + Math.random().toString(36).slice(2), type: 'info', message: '', duration: 4200, paused: false }, t);
  item.remaining = item.duration;
  item.start = Date.now();
  queue.toasts.push(item);
  item.timer = setTimeout(function () { queue.dismiss(item.id); }, item.remaining);
};
queue.pause = function (t) {
  if (t.paused) return;
  t.paused = true;
  clearTimeout(t.timer);
  t.remaining -= Date.now() - t.start;
};
queue.resume = function (t) {
  if (!t.paused) return;
  t.paused = false;
  t.start = Date.now();
  t.timer = setTimeout(function () { queue.dismiss(t.id); }, t.remaining);
};
queue.dismiss = function (id) {
  queue.toasts = queue.toasts.filter(function (t) { return t.id !== id; });
};`
}

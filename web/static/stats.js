// Sends the page's events to Umami, loaded just before this: a click on a
// link marked data-event, a theme picked in the theme switcher, and, on the
// not-found page, the missing path and the page that linked to it. Each
// data-event-<key> attribute adds <key> to the event's data.
//
// Umami's own data-umami-event attributes hold a link until Umami's request
// comes back. Umami sends with keepalive, so the request outlives the page,
// and these let the link go at once.
(() => {
  const track = (name, data) => window.umami?.track(name, data);
  const dataOf = (element) =>
    Object.fromEntries(
      element
        .getAttributeNames()
        .filter((name) => name.startsWith("data-event-"))
        .map((name) => [name.slice("data-event-".length), element.getAttribute(name)]),
    );

  // A middle click opens a link too, as an auxclick; a right click opens a menu.
  for (const type of ["click", "auxclick"]) {
    document.addEventListener(
      type,
      (event) => {
        const link = event.button <= 1 && event.target.closest("a[data-event]");
        if (link) track(link.dataset.event, dataOf(link));
      },
      true,
    );
  }

  document.addEventListener("change", (event) => {
    if (event.target.matches("input[data-event]")) track(event.target.dataset.event, dataOf(event.target));
  });

  if (document.currentScript?.hasAttribute("data-not-found")) {
    track("404", { path: location.pathname, ...(document.referrer && { referrer: document.referrer }) });
  }
})();

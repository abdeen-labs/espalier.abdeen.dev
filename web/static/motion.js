// Hover wiggles that always finish. Hovering an element marked data-wiggle
// plays the wiggle (.wiggling in site.css) on the child its value names, and
// the wiggle runs to its end even if the pointer leaves first, where a CSS
// :hover animation would snap back. Under Reduce Motion it does nothing.
(() => {
  if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
  for (const host of document.querySelectorAll("[data-wiggle]")) {
    const target = host.querySelector(host.dataset.wiggle);
    if (!target) continue;
    host.addEventListener("pointerenter", () => target.classList.add("wiggling"));
    target.addEventListener("animationend", (event) => {
      if (event.animationName === "wiggle") target.classList.remove("wiggling");
    });
  }
})();

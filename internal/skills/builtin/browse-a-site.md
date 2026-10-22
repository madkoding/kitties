# Browse a site on your desktop

How to read or use a web page with your own display, step by step, without guessing.

1. `@tool open {"url": "https://…"}` and `@tool wait {"seconds": 2}`: the screenshot that comes
   back shows the page as it loaded.
2. Read the screenshot before acting. Say what you see in one line, then act on it.
3. To follow a link or press a button, click its centre: `@tool click {"x": …, "y": …}`.
   Coordinates are pixels of the 1280x800 display, origin top-left.
4. To type in a field, click it first, then `@tool type {"text": "…"}`; `@tool key {"keys":
   "Return"}` submits.
5. A long page: `@tool scroll {"direction": "down", "amount": 5}` and look again.
6. Finish with `@tool done {"summary": "…"}` that says what you found and what you could not.

Pitfalls: a cookie banner covers the page (close it first); a page still loading looks empty
(wait once more); never type a password you were not given.

// Quotes of WhatsApp replies, as the bridge writes them before a contact's message (internal/whatsapp).
// splitQuote takes the line a WhatsApp reply starts with, "(replying to Ana (+569…): "…")", off its
// text, to show it as a quote.
export function splitQuote(text: string): [string, string] {
  if (!text.startsWith("(replying to ")) {
    return ["", text];
  }
  const nl = text.indexOf("\n");
  if (nl < 0) {
    return [text.slice(1, -1), ""];
  }
  const line = text.slice(0, nl);
  return [line.endsWith(")") ? line.slice(1, -1) : line, text.slice(nl + 1)];
}

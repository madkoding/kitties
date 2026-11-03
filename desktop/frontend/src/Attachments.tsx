import { useEffect, useState } from "preact/hooks";
import type { Attachment } from "./api";
import {
  ATTACH_MAX_FILE,
  ATTACH_MAX_FILES,
  ATTACH_MAX_IMAGE,
  ATTACH_MAX_TOTAL,
  attachmentPath,
  inWails,
  shotData,
  shotURL,
} from "./api";
import { t } from "./i18n";
import { IconFile, IconX } from "./icons";

// fileSize is a size in words people read: 340 KB, 1.2 MB.
export function fileSize(n: number): string {
  return n >= 1 << 20
    ? `${(n / (1 << 20)).toFixed(1)} MB`
    : `${Math.max(1, Math.round(n / 1024))} KB`;
}

const IMAGE = /^image\/(png|jpeg|gif|webp)$/;

// checkFiles adds files to the ones picked, within what a message may carry. It returns the list
// and, when something was left out, why. The gateway checks again by each file's real type.
export function checkFiles(have: File[], added: File[]): { files: File[]; error: string } {
  const files = [...have];
  let error = "";
  let total = have.reduce((n, f) => n + f.size, 0);
  for (const f of added) {
    const limit = IMAGE.test(f.type) ? ATTACH_MAX_IMAGE : ATTACH_MAX_FILE;
    if (files.length >= ATTACH_MAX_FILES) {
      error = t("A message takes at most {n} files.", { n: ATTACH_MAX_FILES });
    } else if (f.size === 0) {
      error = t("{name} is empty.", { name: f.name });
    } else if (f.size > limit) {
      error = t("{name} is {size}; the most for this kind of file is {max}.", {
        name: f.name,
        size: fileSize(f.size),
        max: fileSize(limit),
      });
    } else if (total + f.size > ATTACH_MAX_TOTAL) {
      error = t("The files add up to more than {max}.", { max: fileSize(ATTACH_MAX_TOTAL) });
    } else {
      files.push(f);
      total += f.size;
    }
  }
  return { files, error };
}

// Picked is a file waiting in the composer, as a chip: a thumbnail for a picture, an icon for the rest.
export function Picked({ file, onRemove }: { file: File; onRemove: () => void }) {
  const [src, setSrc] = useState("");
  useEffect(() => {
    if (!IMAGE.test(file.type)) {
      return;
    }
    const url = URL.createObjectURL(file);
    setSrc(url);
    return () => URL.revokeObjectURL(url);
  }, [file]);
  return (
    <span class="attach-chip" data-testid="picked-file" title={file.name}>
      {src ? <img src={src} alt="" /> : <IconFile size={16} />}
      <span class="attach-name">{file.name}</span>
      <span class="attach-size">{fileSize(file.size)}</span>
      <button
        type="button"
        class="plain attach-x"
        onClick={onRemove}
        aria-label={t("Remove {name}", { name: file.name })}
      >
        <IconX size={12} />
      </button>
    </span>
  );
}

// Sent shows the files of a message: a picture as a thumbnail that opens big, the rest as a chip
// that downloads (in the browser; the native app cannot save from a link, so it only names it).
export function Sent({
  chat,
  files,
  onOpen,
}: {
  chat: string;
  files: Attachment[];
  onOpen: (src: string) => void;
}) {
  return (
    <div class="attach-list" data-testid="sent-files">
      {files.map((a) =>
        a.kind === "image" ? (
          <SentImage key={a.id} chat={chat} file={a} onOpen={onOpen} />
        ) : inWails() ? (
          <span key={a.id} class="attach-chip" title={a.name}>
            <IconFile size={16} />
            <span class="attach-name">{a.name}</span>
            <span class="attach-size">{fileSize(a.size)}</span>
          </span>
        ) : (
          <a
            key={a.id}
            class="attach-chip"
            href={shotURL(attachmentPath(chat, a.id))}
            download={a.name}
            title={t("Download {name}", { name: a.name })}
          >
            <IconFile size={16} />
            <span class="attach-name">{a.name}</span>
            <span class="attach-size">{fileSize(a.size)}</span>
          </a>
        ),
      )}
    </div>
  );
}

function SentImage({
  chat,
  file,
  onOpen,
}: {
  chat: string;
  file: Attachment;
  onOpen: (src: string) => void;
}) {
  const [src, setSrc] = useState("");
  useEffect(() => {
    let alive = true;
    shotData(attachmentPath(chat, file.id), file.mime)
      .then((s) => alive && setSrc(s))
      .catch(() => undefined);
    return () => {
      alive = false;
    };
  }, [chat, file.id, file.mime]);
  return (
    <button
      type="button"
      class="plain attach-thumb"
      onClick={() => src && onOpen(src)}
      aria-label={t("Open {name}", { name: file.name })}
      title={file.name}
    >
      {src && <img src={src} alt={file.name} />}
    </button>
  );
}

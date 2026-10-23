// Required marks a field the form cannot go on without; the field itself carries aria-required.
export function Required() {
  return (
    <span class="req" title="Required" aria-hidden="true">
      *
    </span>
  );
}

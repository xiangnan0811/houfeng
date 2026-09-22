type ColumnResizeHandleProps = {
  onDragStart: (clientX: number) => void
}

export function ColumnResizeHandle({ onDragStart }: ColumnResizeHandleProps) {
  return (
    <span
      className="col-resize"
      aria-hidden="true"
      onPointerDown={(event) => {
        event.preventDefault()
        event.stopPropagation()
        onDragStart(event.clientX)
      }}
    />
  )
}

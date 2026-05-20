import { ChevronLeft, ChevronRight } from "lucide-react";

export function Pager({
  page,
  offset,
  setOffset,
}: {
  page: { total: number; limit: number };
  offset: number;
  setOffset: (value: number) => void;
}) {
  const start = page.total === 0 ? 0 : offset + 1;
  const end = Math.min(offset + page.limit, page.total);
  const canPrev = offset > 0;
  const canNext = offset + page.limit < page.total;
  return (
    <div className="pager">
      <span>
        {start}-{end} / {page.total}
      </span>
      <div className="inlineActions">
        <button
          className="iconButton"
          onClick={() => setOffset(Math.max(0, offset - page.limit))}
          disabled={!canPrev}
          title="上一页"
          type="button"
        >
          <ChevronLeft size={17} />
        </button>
        <button
          className="iconButton"
          onClick={() => setOffset(offset + page.limit)}
          disabled={!canNext}
          title="下一页"
          type="button"
        >
          <ChevronRight size={17} />
        </button>
      </div>
    </div>
  );
}

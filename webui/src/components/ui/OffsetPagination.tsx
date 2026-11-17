import { useState } from "react";
import { Button } from "./Button";
import { Input } from "./Input";
import { Select } from "./Select";
import { useI18n } from "../../i18n";

type OffsetPaginationProps = {
  page: number;
  totalPages: number;
  totalItems: number;
  pageSize: number;
  pageSizeOptions: readonly number[];
  disabled?: boolean;
  onPageChange: (page: number) => void;
  onPageSizeChange: (pageSize: number) => void;
};

export function OffsetPagination({
  page,
  totalPages,
  totalItems,
  pageSize,
  pageSizeOptions,
  disabled = false,
  onPageChange,
  onPageSizeChange,
}: OffsetPaginationProps) {
  const { t } = useI18n();
  const normalizedTotalPages = Math.max(1, totalPages);
  const normalizedPage = Math.min(Math.max(page, 0), normalizedTotalPages - 1);
  const pageStart = totalItems === 0 ? 0 : normalizedPage * pageSize + 1;
  const pageEnd = Math.min((normalizedPage + 1) * pageSize, totalItems);

  const [jumpValue, setJumpValue] = useState<string | null>(null);

  const jumpTo = (nextPage: number) => {
    setJumpValue(null);
    const bounded = Math.min(Math.max(nextPage, 0), normalizedTotalPages - 1);
    onPageChange(bounded);
  };

  return (
    <div className="nodes-pagination">
      <p className="nodes-pagination-meta">
        {t("第 {{page}} / {{pages}} 页 · 显示 {{start}}-{{end}} / {{total}}", {
          page: normalizedPage + 1,
          pages: normalizedTotalPages,
          start: pageStart,
          end: pageEnd,
          total: totalItems,
        })}
      </p>
      <div className="nodes-pagination-controls">
        <label className="nodes-page-size">
          <span>{t("每页")}</span>
          <Select disabled={disabled} value={String(pageSize)} onChange={(event) => onPageSizeChange(Number(event.target.value))}>
            {pageSizeOptions.map((size) => (
              <option key={size} value={size}>
                {size}
              </option>
            ))}
          </Select>
        </label>

        <label className="nodes-page-jump">
          <span>{t("跳至")}</span>
          <Input
            type="number"
            min={1}
            max={normalizedTotalPages}
            step={1}
            value={jumpValue ?? String(normalizedPage + 1)}
            onChange={(event) => setJumpValue(event.target.value)}
            onBlur={(event) => {
              const value = Number(event.target.value);
              if (event.target.value && Number.isFinite(value)) jumpTo(Math.trunc(value) - 1);
              else setJumpValue(null);
            }}
            onKeyDown={(event) => { if (event.key === "Enter") event.currentTarget.blur(); }}
            aria-label={t("选择页码")}
            disabled={disabled}
          />
          <span>{t("页")}</span>
        </label>

        <Button variant="secondary" size="sm" onClick={() => jumpTo(normalizedPage - 1)} disabled={disabled || normalizedPage <= 0}>
          {t("上一页")}
        </Button>
        <Button
          variant="secondary"
          size="sm"
          onClick={() => jumpTo(normalizedPage + 1)}
          disabled={disabled || normalizedPage >= normalizedTotalPages - 1}
        >
          {t("下一页")}
        </Button>
      </div>
    </div>
  );
}

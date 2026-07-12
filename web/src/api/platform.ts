import request from "@/utils/request";
export const deviceApi = {
  list: (params = {}) => request.get("/devices", { params }),
  create: (data: unknown) => request.post("/devices", data),
  update: (id: string, data: unknown) => request.put(`/devices/${id}`, data),
  remove: (id: string) => request.delete(`/devices/${id}`),
  protocols: () => request.get("/devices/protocols"),
};
export const collectionApi = {
  list: (params = {}) => request.get("/collection-points", { params }),
  create: (data: unknown) => request.post("/collection-points", data),
  update: (id: string, data: unknown) =>
    request.put(`/collection-points/${id}`, data),
  remove: (id: string) => request.delete(`/collection-points/${id}`),
  groups: () => request.get("/collection-points/groups"),
  createGroup: (name: string) =>
    request.post("/collection-points/groups", { name }),
  updateGroup: (old_name: string, name: string) =>
    request.put("/collection-points/groups", { old_name, name }),
  removeGroup: (name: string) =>
    request.delete(`/collection-points/groups/${encodeURIComponent(name)}`),
};
export const writePointApi = {
  list: (params = {}) => request.get("/write-points", { params }),
  create: (data: unknown) => request.post("/write-points", data),
  update: (id: string, data: unknown) =>
    request.put(`/write-points/${id}`, data),
  remove: (id: string) => request.delete(`/write-points/${id}`),
  write: (id: string, data: unknown) =>
    request.post(`/write-points/${id}/write`, data),
  logs: (params = {}) => request.get("/write-logs", { params }),
};
export const historyApi = {
  tree: () => request.get("/history/tree"),
  cleanupArchives: () => request.post("/history/archive/cleanup"),
  query: (data: unknown) => request.post("/history/query", data),
  queryTable: (data: unknown) => request.post("/history/query-table", data),
};
export const systemApi = {
  getRetention: () => request.get("/system/history-retention"),
  setRetention: (history_retention_days: number) =>
    request.put("/system/history-retention", { history_retention_days }),
};
export async function exportConfig(
  kind: "devices" | "collection-points" | "write-points",
) {
  const r = await fetch(`/api/v1/${kind}/export`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: "{}",
  });
  if (!r.ok) throw new Error("导出失败");
  const blob = await r.blob(),
    a = document.createElement("a");
  a.href = URL.createObjectURL(blob);
  a.download = `${kind}.csv`;
  a.click();
  URL.revokeObjectURL(a.href);
}
export async function importConfig(
  kind: "devices" | "collection-points" | "write-points",
  file: File,
) {
  const body = new FormData();
  body.append("file", file);
  const r = await fetch(`/api/v1/${kind}/import`, { method: "POST", body });
  const data = await r.json();
  if (!r.ok) throw new Error(data.message ?? "导入失败");
  return data;
}

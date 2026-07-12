import axios from "axios";

const request = axios.create({ baseURL: "/api/v1", timeout: 15000 });
request.interceptors.response.use(
  (response) => response.data,
  (error) =>
    Promise.reject(new Error(error.response?.data?.message ?? "请求失败")),
);
export default request;

/** 后端统一的分页响应结构 */
export interface PageResult<T> {
  list: T[];
  page: number;
  pageSize: number;
  total: number;
}

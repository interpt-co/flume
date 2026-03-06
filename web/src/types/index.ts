export interface KubeMeta {
  namespace: string
  pod: string
  container: string
  pod_uid?: string
  node_name?: string
}

export interface LogMessage {
  id: string
  content: string
  json_content?: unknown
  is_json: boolean
  ts: string // ISO timestamp
  source: 'loki' | 'stdin' | 'file' | 'socket' | 'demo' | 'forward' | 'container'
  origin: Origin
  labels?: Record<string, string>
  level?: string
  kube?: KubeMeta
}

export interface Origin {
  name: string
  meta?: Record<string, string>
}

export interface WSMessage {
  type: string
  data?: unknown
}

export interface ClientJoinedData {
  client_id: string
  buffer_size: number
  patterns?: string[]
  default_pattern?: string
  pre_filters?: Record<string, string>
}

export interface LogBulkData {
  messages: LogMessage[]
  total: number
}

export interface StatusData {
  clients: number
  messages: number
  buffer_used: number
  buffer_capacity: number
}

export interface PatternChangedData {
  pattern: string
  buffer_size: number
  buffer_used: number
}

export interface PatternInfo {
  name: string
  buffer_used: number
  buffer_capacity: number
  message_count: number
  subscriber_count: number
}

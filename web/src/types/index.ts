export interface LogMessage {
  id: string
  content: string
  json_content?: unknown
  is_json: boolean
  ts: string // ISO timestamp
  source: 'loki' | 'stdin' | 'file' | 'socket' | 'demo' | 'forward'
  origin: Origin
  labels?: Record<string, string>
  level?: string
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

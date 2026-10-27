import { apiClient } from './client'

export interface BillModel {
  model: string
  requests: string
  tokens: string
  images: string
  video_seconds: string
  videos: string
  cost: string
}
export interface BillDay {
  date: string
  consumption: string
  rebate: string
  net: string
  models: BillModel[]
  transfers: { id: number; at: string; amount: string }[]
}
export interface MonthlyBill {
  month: string
  version: number
  user_id: number
  email: string
  generated_at: string
  start: string
  end: string
  partial: boolean
  timezone: string
  cny_per_credit: string
  policy: string
  consumption: string
  rebate: string
  net: string
  cny: string
  subscription: string
  models: BillModel[]
  subscription_models: BillModel[]
  days: BillDay[]
}
export interface BillVersion { version: number; generated_at: string }

export const monthlyBillsAPI = {
  async list(month: string) {
    return (await apiClient.get<BillVersion[]>('/user/bills', { params: { month } })).data
  },
  async get(month: string, version: number) {
    return (await apiClient.get<MonthlyBill>(`/user/bills/${month}/${version}`)).data
  },
  async generate(month: string, baseVersion: number) {
    return (await apiClient.post<MonthlyBill>('/user/bills', { month, base_version: baseVersion }, { timeout: 35000 })).data
  },
}

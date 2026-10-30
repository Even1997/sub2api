import bills from './bills'
import openaiAuth from './openaiAuth'
import upstreamEntities from './upstreamEntities'
import profitOps from './profitOps'
import upstreamOps from './upstreamOps'
import channelOps from './channelOps'
import ipAudit from './ipAudit'
import traffic from './traffic'
import connections from './connections'
import securityRecords from './securityRecords'
import siwc from './siwc'
import priorityScheduling from './priorityScheduling'
import qualityOps from './qualityOps'
import controlledExperiments from './controlledExperiments'
import accountOps from './accountOps'
import requestTiming from './requestTiming'
import tokenGuard from './tokenGuard'
import tokenGuardV2 from './tokenGuardV2'
import pelicanTests from './pelicanTests'
import modelTrace from './modelTrace'
import adminAccessKeys from './adminAccessKeys'
import landing from './landing'
import common from './common'
import dashboard from './dashboard'
import channelMonitorV2 from './channelMonitorV2'
import channelMonitorV3 from './channelMonitorV3'
import supportTickets from './supportTickets'
import batchImage from './batchImage'
import keyPackages from './keyPackages'
import admin from './admin'
import misc from './misc'

import autoConfig from './autoConfig'

export default {
  bills,
  openaiAuth,
  upstreamEntities,
  traffic,
  ipAudit,
  connections,
  securityRecords,
  siwc,
  autoConfig,
  priorityScheduling,
  qualityOps,
  channelOps,
  upstreamOps,
  profitOps,
  controlledExperiments,
  accountOps,
  tokenGuard,
  tokenGuardV2,
  pelicanTests,
  modelTrace,
  requestTiming,
  adminAccessKeys,
  ...landing,
  ...common,
  ...dashboard,
  ...channelMonitorV2,
  ...channelMonitorV3,
  ...supportTickets,
  ...batchImage,
  ...keyPackages,
  admin,
  ...misc,
}

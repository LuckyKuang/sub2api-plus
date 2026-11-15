import workspace from './workspace'
import requestList from './requestList'
import featureExtensions from './featureExtensions'
import adminAccessKeys from './adminAccessKeys'
import codexCli from './codexCli'
import codexCliRuntime from './codexCliRuntime'

import mcp from './mcp'
import landing from './landing'
import common from './common'
import dashboard from './dashboard'
import channelMonitorV2 from './channelMonitorV2'
import channelMonitorV3 from './channelMonitorV3'
import batchImage from './batchImage'
import asyncImage from './asyncImage'
import imageTasks from './imageTasks'
import admin from './admin'
import misc from './misc'
import inspection from './inspection'
import billingRates from './billingRates'
import nativeAccess from './nativeAccess'

export default {
  ...workspace,
  ...requestList,
  ...inspection,
  ...billingRates,
  ...nativeAccess,
  ...featureExtensions,
  ...codexCli,
  ...codexCliRuntime,
  adminAccessKeys,

  mcp,
  ...landing,
  ...common,
  ...dashboard,
  ...channelMonitorV2,
  ...channelMonitorV3,
  ...batchImage,
  ...asyncImage,
  ...imageTasks,
  admin,
  ...misc,
}

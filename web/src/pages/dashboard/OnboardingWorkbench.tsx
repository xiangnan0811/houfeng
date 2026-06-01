import { Link } from 'react-router-dom'

import { MonoDigits } from '../../components/atoms'
import { DASHBOARD_LINKS } from './dashboardLinks'

export function OnboardingWorkbench() {
  const steps = [
    {
      title: '接入监控实例',
      description: '登记第一台服务器。',
      to: DASHBOARD_LINKS.monitoring,
      cta: '接入第一个监控实例',
    },
    {
      title: '接入 agent',
      description: '进入监控实例详情完成 agent 接入。',
      to: DASHBOARD_LINKS.monitoringPendingOnboarding,
      cta: '查看监控实例接入',
    },
    {
      title: '创建目标',
      description: '添加需要观测的服务或端口。',
      to: DASHBOARD_LINKS.targets,
      cta: '创建第一个目标',
    },
    {
      title: '添加 ProbeItem',
      description: '在目标详情中补齐探测项。',
      to: DASHBOARD_LINKS.targets,
      cta: '添加 ProbeItem',
    },
  ]

  return (
    <div className="dashboard-onboarding">
      {steps.map((step, index) => (
        <article className="dashboard-onboarding__step" key={step.title}>
          <span className="dashboard-onboarding__index">
            <MonoDigits>{index + 1}</MonoDigits>
          </span>
          <div className="dashboard-onboarding__body">
            <h3>{step.title}</h3>
            <p>{step.description}</p>
            <Link className="text-link" to={step.to}>
              {step.cta}
            </Link>
          </div>
        </article>
      ))}
    </div>
  )
}

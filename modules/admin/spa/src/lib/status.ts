import type { BadgeVariant } from "@/components/ui/badge"

export function identityStatusVariant(status: string): BadgeVariant {
  switch (status) {
    case "active":
      return "success"
    case "pending":
      return "warning"
    case "suspended":
      return "destructive"
    default:
      return "secondary"
  }
}

export function mfaVariant(enabled: boolean): BadgeVariant {
  return enabled ? "success" : "warning"
}

export function operatorRoleVariant(role: string): BadgeVariant {
  switch (role) {
    case "admin":
      return "destructive"
    case "operator":
      return "warning"
    case "viewer":
      return "info"
    default:
      return "secondary"
  }
}

export function operatorStatusVariant(status: string): BadgeVariant {
  return status === "active" ? "success" : "destructive"
}

export function healthStatusVariant(status: string): BadgeVariant {
  switch (status) {
    case "healthy":
      return "success"
    case "degraded":
      return "warning"
    case "offline":
      return "destructive"
    default:
      return "secondary"
  }
}

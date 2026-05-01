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
    case "super_admin":
      return "destructive"
    case "admin":
      return "warning"
    case "operator":
      return "info"
    case "viewer":
      return "secondary"
    default:
      return "outline"
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

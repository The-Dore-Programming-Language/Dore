"""A hand-written implementation of examples/refunds.dore.

Doré does not require that generated code be the only kind it verifies. The
touchstone gates any implementation, so a spec is useful as a checker for code
you wrote yourself long before generation exists.

    dore assay examples/refunds.dore --impl examples/refunds.py
"""

from decimal import Decimal

MANAGER_APPROVAL_THRESHOLD = Decimal("500.00")
REFUND_WINDOW_DAYS = 30


def refund_eligible(days_since_purchase: int, order_total: Decimal) -> bool:
    if order_total > MANAGER_APPROVAL_THRESHOLD:
        return False
    return days_since_purchase <= REFUND_WINDOW_DAYS

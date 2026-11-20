"""Tiny report generator."""
import sys


def average(values):
    if not values:
        return 0
    return sum(values) / len(values)


def maximum(values):
    return max(values)


def main():
    numbers = [3, 5, 8, 13, 21]
    print("average:", average(numbers))
    print("maximum:", maximum(numbers))


if __name__ == "__main__":
    sys.exit(main())

"""Tiny report generator."""
import sys


def average(values):
    return sum(values) / len(values)


def main():
    numbers = [3, 5, 8, 13]
    print("average:", average(numbers))


if __name__ == "__main__":
    sys.exit(main())

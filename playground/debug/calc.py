"""A small program to try the debugger: put a breakpoint in average() and press ctrl+f5."""


def add(a, b):
    total = a + b
    return total


def average(numbers):
    result = 0
    for number in numbers:
        result = add(result, number)
    return result / len(numbers)


prices = {"apple": 1.2, "pear": 0.8, "plum": 2.5}
print("prices:", prices)
mean = average(list(prices.values()))
print("average price:", mean)

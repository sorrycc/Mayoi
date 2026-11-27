# Math

A fixture for the display formulas of the preview: a matrix product whose
row is a line of `=` alone, a sum that MathJax sets text in, and a line of
`-` alone, which Goldmark reads as a heading underline, a rule, or a table's
delimiter row.

## Inline formulas

A paragraph may hold one: $E = mc^2$ and $\pi r^2$, and the dollars
that are not math, as $5 and $6, stay text.

## A matrix product

$$
\mathbf{A}\mathbf{x} =
\begin{bmatrix}
a_{11} & a_{12} \\
a_{21} & a_{22}
\end{bmatrix}
\begin{bmatrix}
x_1 \\
x_2
\end{bmatrix}
=
\begin{bmatrix}
a_{11}x_1 + a_{12}x_2 \\
a_{21}x_1 + a_{22}x_2
\end{bmatrix}
$$

## A sum set in text

$$
\mathbb{E}[X] = \sum_x xP(X = x)
\qquad\text{或}\qquad
\mathbb{E}[X] = \int_{-\infty}^{\infty} xf(x)\,dx
$$

## A row of a line of -

$$
a
-
b
$$

## A row of three -

$$
a
---
b
$$

## A title after the formulas

Title
===

Another title
---

## A list after the formulas

- one
- two

## A table after the formulas

| kind | formula |
| --- | --- |
| sum | $a + b$ |
| product | $ab$ |

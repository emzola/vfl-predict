import { describe, expect, it } from "vitest";
import { percentage } from "./api";

describe("dashboard formatting", () => {
  it("formats probabilities as readable percentages", () => {
    expect(percentage(0.5)).toBe("50%");
    expect(percentage(0.3333)).toBe("33.3%");
  });
});

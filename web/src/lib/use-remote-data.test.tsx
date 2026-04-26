import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, test } from "vitest";

import { useRemoteData } from "./use-remote-data";

type Deferred<T> = {
  promise: Promise<T>;
  resolve: (value: T) => void;
  reject: (error: Error) => void;
};

function createDeferred<T>(): Deferred<T> {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((nextResolve, nextReject) => {
    resolve = nextResolve;
    reject = nextReject;
  });
  return { promise, resolve, reject };
}

function RemoteDataProbe({
  loaders,
}: {
  loaders: Array<() => Promise<string>>;
}) {
  const [index, setIndex] = useState(0);
  const state = useRemoteData(() => loaders[index](), [index]);

  return (
    <div>
      <button
        type="button"
        onClick={() => {
          setIndex(1);
        }}
      >
        切换
      </button>
      <p>{state.loading ? "加载中" : state.data ?? state.error ?? "空"}</p>
    </div>
  );
}

describe("useRemoteData", () => {
  test("依赖切换后旧响应不会覆盖新结果", async () => {
    const first = createDeferred<string>();
    const second = createDeferred<string>();

    render(
      <RemoteDataProbe
        loaders={[
          () => first.promise,
          () => second.promise,
        ]}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "切换" }));

    second.resolve("第二页");

    expect(await screen.findByText("第二页")).toBeInTheDocument();

    first.resolve("第一页");

    await waitFor(() => {
      expect(screen.getByText("第二页")).toBeInTheDocument();
    });
  });
});

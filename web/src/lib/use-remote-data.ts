import { useEffect, useRef, useState, type DependencyList } from "react";

type RemoteDataState<T> = {
  data: T | null;
  loading: boolean;
  error: string | null;
};

export function useRemoteData<T>(
  load: () => Promise<T>,
  dependencies: DependencyList = [load],
): RemoteDataState<T> {
  const [data, setData] = useState<T | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const requestSequence = useRef(0);

  useEffect(() => {
    let active = true;
    const currentRequest = requestSequence.current + 1;
    requestSequence.current = currentRequest;

    async function run() {
      setLoading(true);
      setError(null);

      try {
        const next = await load();

        if (!active || requestSequence.current != currentRequest) {
          return;
        }

        setData(next);
        setError(null);
      } catch (error) {
        if (!active || requestSequence.current != currentRequest) {
          return;
        }

        setError(error instanceof Error ? error.message : "加载失败，请稍后重试。");
      } finally {
        if (active && requestSequence.current == currentRequest) {
          setLoading(false);
        }
      }
    }

    void run();

    return () => {
      active = false;
    };
  }, dependencies);

  return { data, loading, error };
}

# Why does this project exists...

This code was adopted from the loadwatcher to reach cpu utilizations above 50%
without impacting tail latencies of pods.

## What's wrong with cpu requests (and cores)

**Q: What do you get if you set `requests.cpu: 1.0`?**

A simple question that is often answered with "1 core". But what does it mean?

- One core is not comparable between different cpu as each cpu has a different runtime behavior
- One core is not comparable within one cpu type, as the microcode, BIOS & OS revision may impact the performance (spectre anyone?)
- One core is not comparable even with the same microcode, BIOS & OS due to power or thermal constraints
- One core is not comparable even under fixed frequency as it usually means one thread and a threads execution speed depends on the load of the core
- One core might be comparable with fixed frequency, hand-picked microcode, BIOS and OS as well as tight thermal control and removing any other loads. Unless you are plagued by a kernel bug.

So what _do_ we get when setting `requests.cpu: 1.0`? Kubernetes uses CFS and cgroups. Setting your requests to 1.0 means you will get 1024 shares
of CPU time, which will guarantee `1/nth` of the available CPU time where `n` is the number of cpu threads.

**On a 16 core / 32 thread machine you will get ~1/32 of the available CPU.**

This is a lower bound and as the machine gets loaded you will experience that

1. The `requests.cpu` turn into `limits.cpu` as CPU becomes scarce
1. The throughput of the cpus will drop due to thermal and power pressure
1. The throughput of the cpus will drop due to hyperthreading congestion
1. The throughput of the cpus will drop due to higher L1/L2/... cache pressure

The key factors to make this work under high utilization are:

1. `requests.cpu` should reflect empirical and historical cpu usage. E.g. the p75 over all pods of the p75 cpu consumption of each pod over one day.
1. CPU utilization should be stable over time. This implies the use of HPAs for services with traffic curves.

If both conditions are met then our service will be less impacted by high CPU utilization.

## Is there a problem?

The right way to answer if there is an issue is to look at user impact.
In the k8s cpu scheduling case the "users" are pods that want CPU.

The cpu pressure describes how often ther wasn't enough CPU to serve all processes.
A cpu pressure of 20% means that 80% of the time all pods got the cpu they wanted.

We can thus limit the impact of cpu starvation if we limit the cpu pressure.

This is **what kubernetes-pressurecooker does: It limits the cpu pressure by first
blocking new workloads (taint) and then load shedding (eviction) old workloads**.

## What's wrong with `limits.cpu`?

`kubernetes-pressurecooker` tries to remove cpu induced tail latencies by removing CPU wait times.

CPU limits on the other hand are added latency:

- If there is CPU congestion: you are limited by your cpu requests anyway
- If there is no CPU congestion: you get paused if you hit your cpu limits (latency spike!)

Best case they do nothing, worst case they cause latency.

CPU limits should only be used if there is no negative user impact in pausing your process for
several time slices at a time.

## What's wrong with `requests.cpu < 1.0`?

There is no such thing as half-a-cpu. The way this works is that you get cpu 50% of the time.
This can be problematic during CPU congestion.

Try to reach at least `requests.cpu: 1.0` for best tail latency behavior.

## Be careful with default kernels

Most kernels are tweaked for a "good" balance between throughput and latency. Most microservice
systems break if latencies go up but will need more hardware if throughput drops.

The right tradeoff for most microservice and web application backends is lowlatency tweaking,
not throughput tweaking. Waiting for a website to render or for an App View to load is interactive
traffic and should be pushed through with the lowest possible latency variation.

Use smaller time slices by default. Check runqlat to get a sense of cpu wait time, especially on
cpu congested nodes. Try to get predictable time-to-cpu as the load goes up.

## Safety

Reaching >50% utilization safely is hard due to the non-linear nature of CPU usage and the
increased risk of CPU congestion (and thus pressure).

However I have seen pressurecooker enabled clusters exceed 90% cpu utilization on sudden traffic
surges without significant user impact.
Most nodes were tainted and HPAs got busy looking for new nodes - but the overall system was stable.

This was supported by several safety mechanisms:

- eviction kicks in at a way higher thresholds than tainting
- eviction is slow (rate limited, no eviction of new pods)

Active load shedding is both risky (can the pod come up elsewhere?) and required. The alternative
would be potential p90 latency spikes from pods on the impacted node. 

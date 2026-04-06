# Pub/Sub Flow

How messages travel from a `PUBLISH` call through the `Broker` to every subscribed client's TCP connection.

## Component Relationships

```mermaid
flowchart LR
    subgraph PUBLISHER["Publisher Client"]
        PUB["redis-cli\nPUBLISH news hello"]
    end

    subgraph SERVER["internal/server"]
        HP["handler.go\n(publisher)\nrouter.Dispatch()"]
        HS1["handler.go\n(subscriber 1)\nwrite loop goroutine"]
        HS2["handler.go\n(subscriber 2)\nwrite loop goroutine"]
        HS3["handler.go\n(subscriber 3 — pattern)\nwrite loop goroutine"]
    end

    subgraph CMD["internal/commands"]
        PCMD["pubsub.go\nhandlePublish()\nPub.Publish(ch, msg)"]
    end

    subgraph PUBSUB["internal/pubsub"]
        BROKER["broker.go\nBroker\n──────────────\nchannels map[string][]*Subscriber\npatterns map[string][]*Subscriber\nmu sync.RWMutex"]
        SUB1["subscriber.go\nSubscriber 1\ninbox chan(256)\ndone  chan"]
        SUB2["subscriber.go\nSubscriber 2\ninbox chan(256)\ndone  chan"]
        SUB3["subscriber.go\nSubscriber 3\n(pattern 'new*')\ninbox chan(256)\ndone  chan"]
    end

    subgraph SUBSCRIBERS["Subscriber Clients"]
        C1["Client 1\nSUBSCRIBE news"]
        C2["Client 2\nSUBSCRIBE news"]
        C3["Client 3\nPSUBSCRIBE new*"]
    end

    PUB -->|"PUBLISH news hello"| HP
    HP --> PCMD
    PCMD -->|"Publish('news','hello')"| BROKER
    BROKER -->|"Send(message_frame)"| SUB1
    BROKER -->|"Send(message_frame)"| SUB2
    BROKER -->|"pattern match\nnew* matches news\nSend(pmessage_frame)"| SUB3
    SUB1 -->|"inbox channel"| HS1
    SUB2 -->|"inbox channel"| HS2
    SUB3 -->|"inbox channel"| HS3
    HS1 -->|"*3\r\nmessage\r\nnews\r\nhello\r\n"| C1
    HS2 -->|"*3\r\nmessage\r\nnews\r\nhello\r\n"| C2
    HS3 -->|"*4\r\npmessage\r\nnew*\r\nnews\r\nhello\r\n"| C3
```

## SUBSCRIBE Sequence

```mermaid
sequenceDiagram
    autonumber
    actor C1 as Client 1
    participant H as handler.go
    participant B as broker.go<br/>Broker
    participant SUB as subscriber.go<br/>Subscriber

    C1->>H: SUBSCRIBE news sports

    Note over H: serve() switch:<br/>case "SUBSCRIBE" → handleSubscribe()

    H->>H: ensureSubscriber()<br/>if sub == nil:<br/>  sub = NewSubscriber()<br/>  go writeLoop()

    Note over H,SUB: First subscription creates Subscriber + write loop goroutine

    loop for each channel: news, sports
        H->>B: broker.Subscribe(channel, sub)
        B->>B: mu.Lock()<br/>channels[ch] = append(channels[ch], sub)<br/>mu.Unlock()
        B-->>H: (done)
        H->>H: subChans[ch] = struct{}{}
        H->>SUB: sub.Send(EncodeSubscribeAck(ch, totalCount))
        SUB-->>C1: *3\r\n$9\r\nsubscribe\r\n$4\r\nnews\r\n:1\r\n
    end
```

## PUBLISH Sequence

```mermaid
sequenceDiagram
    autonumber
    actor PUB as Publisher
    participant HP as handler.go<br/>(publisher conn)
    participant R as router.go
    participant PC as pubsub.go<br/>handlePublish()
    participant B as broker.go<br/>Broker
    participant S1 as Subscriber 1<br/>inbox chan
    participant S2 as Subscriber 2<br/>inbox chan (pattern)
    participant H1 as handler.go<br/>write loop 1
    participant H2 as handler.go<br/>write loop 2
    actor C1 as Client 1
    actor C2 as Client 2

    PUB->>HP: PUBLISH news "hello world"
    HP->>R: Dispatch(["PUBLISH","news","hello world"])
    R->>PC: handlePublish(args, store)
    PC->>B: pub.Publish("news", "hello world")

    B->>B: mu.RLock()<br/>snap exact subscribers for "news"<br/>snap pattern subscribers matching "news"<br/>mu.RUnlock()

    Note over B: Fan-out happens OUTSIDE lock<br/>snapshot prevents holding lock during sends

    B->>S1: Send(encodeMessage("news","hello world"))
    Note right of S1: non-blocking select:<br/>inbox <- msg or drop if full (256 cap)

    B->>S2: Send(encodePMessage("new*","news","hello world"))

    S1->>H1: msg via inbox channel
    S2->>H2: msg via inbox channel

    H1-->>C1: *3\r\n$7\r\nmessage\r\n$4\r\nnews\r\n$11\r\nhello world\r\n
    H2-->>C2: *4\r\n$8\r\npmessage\r\n$4\r\nnew*\r\n$4\r\nnews\r\n$11\r\nhello world\r\n

    PC-->>R: Integer(2)   ← subscriber count
    R-->>HP: Response
    HP-->>PUB: :2\r\n
```

## Write Loop & Cleanup

`sub.inbox` is **never closed** — only `sub.done` is closed. This prevents a panic if a concurrent `Publish` sends to the channel just as the subscriber is shutting down.

```mermaid
flowchart TB
    subgraph WL["writeLoop() goroutine — sole writer to conn after first SUBSCRIBE"]
        SEL{"select"}
        INBOX["← sub.Inbox():\nwrite msg to conn"]
        DONE["← sub.Done():\ndrain remaining msgs\nthen return"]
    end

    subgraph CLEANUP["cleanupSubscriptions() — called via defer in serve()"]
        US["broker.UnsubscribeAll(sub)\nbroker.PUnsubscribeAll(sub)"]
        CL["sub.Close()\n→ close(done channel)\n→ signals writeLoop to exit"]
    end

    SEL --> INBOX
    SEL --> DONE
    INBOX --> SEL
    CLEANUP --> WL
```

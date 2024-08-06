import React, { useEffect, useState } from 'react';

function App() {
  const [data, setData] = useState([]);

  useEffect(() => {
    const ws = new WebSocket('ws://localhost:8080/ws');

    ws.onmessage = (event) => {
      const message = JSON.parse(event.data);
      setData(message); // Assuming message contains an array of key-value pairs
    };

    return () => ws.close();
  }, []);

  return (
    <div>
      <h1>Cache Viewer</h1>
      <ul>
        {data.map((item, index) => (
          <li key={index}>{item.key}: {item.value} (Expires in {item.expiry})</li>
        ))}
      </ul>
    </div>
  );
}

export default App;

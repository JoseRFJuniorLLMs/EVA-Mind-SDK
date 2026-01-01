// pcm-processor.js
class PCMProcessor extends AudioWorkletProcessor {
    constructor() {
        super();
        this.port.onmessage = (event) => {
            // Aqui você pode receber comandos se precisar
        };
    }

    process(inputs, outputs, parameters) {
        const input = inputs[0];
        if (input.length > 0) {
            const inputChannel = input[0];
            if (inputChannel) {
                // Converte Float32 para Int16 (PCM16)
                const pcm16 = new Int16Array(inputChannel.length);
                for (let i = 0; i < inputChannel.length; i++) {
                    const s = inputChannel[i];
                    pcm16[i] = s < 0 ? s * 0x8000 : s * 0x7FFF;
                }

                // Envia o buffer PCM16 para o main thread
                this.port.postMessage(pcm16.buffer, [pcm16.buffer]);
            }
        }

        // Mantém o process rodando
        return true;
    }
}

registerProcessor('pcm-processor', PCMProcessor);